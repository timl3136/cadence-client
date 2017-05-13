package cadence

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/facebookgo/clock"
	m "github.com/uber-go/cadence-client/.gen/go/cadence"
	"github.com/uber-go/cadence-client/.gen/go/shared"
	"github.com/uber-go/cadence-client/common"
	"go.uber.org/atomic"
	"go.uber.org/zap"
)

const (
	defaultTestTaskList   = "default-test-tasklist"
	defaultTestWorkflowID = "default-test-workflow-id"
	defaultTestRunID      = "default-test-run-id"
)

type (
	timerHandle struct {
		callback resultHandler
		timer    *clock.Timer
		duration time.Duration
		timerId  int
	}

	activityHandle struct {
		callback     resultHandler
		activityType string
	}

	callbackHandle struct {
		callback          func()
		startDecisionTask bool // start a new decision task after callback() is handled.
	}

	activityExecutorWrapper struct {
		activity
		env *testWorkflowEnvironmentImpl
	}

	taskListSpecificActivity struct {
		fn        interface{}
		taskLists map[string]struct{}
	}

	// testWorkflowEnvironmentImpl is the environment that runs the workflow/activity unit tests.
	testWorkflowEnvironmentImpl struct {
		testSuite                  *WorkflowTestSuite
		overrodeActivities         map[string]interface{} // map of registered-fnName -> fakeActivityFn
		taskListSpecificActivities map[string]*taskListSpecificActivity

		service       m.TChanWorkflowService
		workerOptions WorkerOptions
		logger        *zap.Logger
		clock         *clock.Mock

		workflowInfo          *WorkflowInfo
		workflowDef           workflowDefinition
		counterID             int
		workflowCancelHandler func()

		locker               *sync.Mutex
		scheduledActivities  map[string]*activityHandle
		scheduledTimers      map[string]*timerHandle
		runningActivityCount atomic.Int32

		onActivityStartedListener func(ctx context.Context, args EncodedValues)
		onActivityEndedListener   func(result EncodedValue, err error, activityType string)
		onTimerScheduledListener  func(timerID string, duration time.Duration)
		onTimerFiredListener      func(timerID string)
		onTimerCancelledListener  func(timerID string)

		callbackChannel chan callbackHandle
		testTimeout     time.Duration
		isTestCompleted bool
		testResult      EncodedValue
		testError       error
	}
)

func (env *testWorkflowEnvironmentImpl) setActivityTaskList(tasklist string, activityFns ...interface{}) {
	for _, activityFn := range activityFns {
		env.testSuite.RegisterActivity(activityFn)
		fnName := getFunctionName(activityFn)
		taskListActivity, ok := env.taskListSpecificActivities[fnName]
		if !ok {
			taskListActivity = &taskListSpecificActivity{fn: activityFn, taskLists: make(map[string]struct{})}
			env.taskListSpecificActivities[fnName] = taskListActivity
		}
		taskListActivity.taskLists[tasklist] = struct{}{}
	}
}

func (env *testWorkflowEnvironmentImpl) executeWorkflow(workflowFn interface{}, args ...interface{}) {
	s := env.testSuite
	var workflowType string
	fnType := reflect.TypeOf(workflowFn)
	switch fnType.Kind() {
	case reflect.String:
		workflowType = workflowFn.(string)
	case reflect.Func:
		// auto register workflow if it is not already registered
		fnName := getFunctionName(workflowFn)
		if _, ok := s.hostEnv.getWorkflowFn(fnName); !ok {
			s.RegisterWorkflow(workflowFn)
		}
		workflowType = getFunctionName(workflowFn)
	default:
		panic("unsupported workflowFn")
	}

	env.workflowInfo.WorkflowType.Name = workflowType
	factory := getWorkflowDefinitionFactory(s.hostEnv.newRegisteredWorkflowFactory())
	workflowDefinition, err := factory(env.workflowInfo.WorkflowType)
	if err != nil {
		// try to get workflow from global registered workflows
		factory = getWorkflowDefinitionFactory(getHostEnvironment().newRegisteredWorkflowFactory())
		workflowDefinition, err = factory(env.workflowInfo.WorkflowType)
		if err != nil {
			panic(err)
		}
	}
	env.workflowDef = workflowDefinition

	input, err := s.hostEnv.encodeArgs(args)
	if err != nil {
		panic(err)
	}
	env.workflowDef.Execute(env, input)

	env.startMainLoop()
}

func (env *testWorkflowEnvironmentImpl) OverrideActivity(activityFn, fakeActivityFn interface{}) {
	// verify both functions are valid activity func
	actualFnType := reflect.TypeOf(activityFn)
	if err := validateFnFormat(actualFnType, false); err != nil {
		panic(err)
	}
	fakeFnType := reflect.TypeOf(fakeActivityFn)
	if err := validateFnFormat(fakeFnType, false); err != nil {
		panic(err)
	}

	// verify signature of registeredActivityFn and fakeActivityFn are the same.
	if actualFnType != fakeFnType {
		panic("activityFn and fakeActivityFn have different func signature")
	}

	fnName := getFunctionName(activityFn)
	env.overrodeActivities[fnName] = fakeActivityFn
}

// startDecisionTask will trigger OnDecisionTaskStart() on the workflow which will execute the dispatcher until all
// coroutinues are blocked. This method is only necessary when you disable the auto start decision task to have full
// control of the workflow execution on when to start a decision task.
func (env *testWorkflowEnvironmentImpl) startDecisionTask() {
	// post an empty callback to event loop, and request OnDecisionTaskStarted to be triggered after that empty callback
	env.postCallback(func() {}, true /* to start decision task */)
}

func (env *testWorkflowEnvironmentImpl) startMainLoop() {
	// kick off the initial decision task
	env.startDecisionTask()

	for {
		// use non-blocking-select to check if there is anything pending in the main thread.
		select {
		case c := <-env.callbackChannel:
			// this will drain the callbackChannel
			env.processCallback(c)
		default:
			// nothing to process, main thread is blocked at this moment, now check if we should auto fire next timer
			if !env.autoFireNextTimer() {
				if env.isTestCompleted {
					return
				}

				// no timer to fire, wait for things to do or timeout.
				select {
				case c := <-env.callbackChannel:
					env.processCallback(c)
				case <-time.After(env.testTimeout):
					st := env.workflowDef.StackTrace()
					env.testSuite.FailNow("Test timeout.", "TestTimeout: %v, DispatcherStack: %v", env.testTimeout, st)
				}
			}
		}
	}
}

func (env *testWorkflowEnvironmentImpl) registerDelayedCallback(f func(), delayDuration time.Duration) {
	env.postCallback(func() {
		env.NewTimer(delayDuration, func(result []byte, err error) {
			f()
		})
	}, true)
}

func (env *testWorkflowEnvironmentImpl) processCallback(c callbackHandle) {
	// locker is needed to prevent race condition between dispatcher loop goroutinue and activity worker goroutinues.
	// The activity workers could call into Heartbeat which by default is mocked in this test suite. The mock needs to
	// access s.scheduledActivities map, that could cause data race warning.
	env.locker.Lock()
	defer env.locker.Unlock()
	c.callback()
	if c.startDecisionTask {
		env.workflowDef.OnDecisionTaskStarted() // this will execute dispatcher
	}
}

func (env *testWorkflowEnvironmentImpl) autoFireNextTimer() bool {
	if len(env.scheduledTimers) == 0 || env.runningActivityCount.Load() > 0 {
		// do not auto forward workflow clock if there is running activities.
		return false
	}

	// find next timer
	var tofire *timerHandle
	for _, t := range env.scheduledTimers {
		if tofire == nil {
			tofire = t
		} else if t.duration < tofire.duration ||
			(t.duration == tofire.duration && t.timerId < tofire.timerId) {
			tofire = t
		}
	}

	d := tofire.duration
	env.logger.Debug("Auto fire timer", zap.Int(tagTimerID, tofire.timerId), zap.Duration("Duration", tofire.duration))

	// Move mock clock forward, this will fire the timer, and the timer callback will remove timer from scheduledTimers.
	env.clock.Add(d)

	// reduce all pending timer's duration by d
	for _, t := range env.scheduledTimers {
		t.duration -= d
	}
	return true
}

func (env *testWorkflowEnvironmentImpl) postCallback(cb func(), startDecisionTask bool) {
	env.callbackChannel <- callbackHandle{callback: cb, startDecisionTask: startDecisionTask}
}

func (env *testWorkflowEnvironmentImpl) RequestCancelActivity(activityID string) {
	handle, ok := env.scheduledActivities[activityID]
	if !ok {
		env.logger.Debug("RequestCancelActivity failed, Activity not exists or already completed.", zap.String(tagActivityID, activityID))
		return
	}
	env.logger.Debug("RequestCancelActivity", zap.String(tagActivityID, activityID))
	delete(env.scheduledActivities, activityID)
	env.postCallback(func() {
		cancelledErr := NewCanceledError()
		handle.callback(nil, cancelledErr)
		if env.onActivityEndedListener != nil {
			env.onActivityEndedListener(nil, cancelledErr, handle.activityType)
		}
	}, true)
}

// RequestCancelTimer request to cancel timer on this testWorkflowEnvironmentImpl.
func (env *testWorkflowEnvironmentImpl) RequestCancelTimer(timerID string) {
	env.logger.Debug("RequestCancelTimer", zap.String(tagTimerID, timerID))
	timerHandle, ok := env.scheduledTimers[timerID]
	if !ok {
		env.logger.Debug("RequestCancelTimer failed, TimerID not exists.", zap.String(tagTimerID, timerID))
	}

	delete(env.scheduledTimers, timerID)
	timerHandle.timer.Stop()
	env.postCallback(func() {
		timerHandle.callback(nil, NewCanceledError())
		if env.onTimerCancelledListener != nil {
			env.onTimerCancelledListener(timerID)
		}
	}, true)
}

func (env *testWorkflowEnvironmentImpl) Complete(result []byte, err error) {
	if env.isTestCompleted {
		env.logger.Debug("Workflow already completed.")
		return
	}
	env.isTestCompleted = true
	env.testResult = EncodedValue(result)
	env.testError = err

	if err == ErrCanceled && env.workflowCancelHandler != nil {
		env.workflowCancelHandler()
	}
}

func (env *testWorkflowEnvironmentImpl) CompleteActivity(taskToken []byte, result interface{}, err error) error {
	if taskToken == nil {
		return errors.New("nil task token provided")
	}
	var data []byte
	if result != nil {
		var encodeErr error
		data, encodeErr = getHostEnvironment().encodeArg(result)
		if encodeErr != nil {
			return encodeErr
		}
	}

	activityID := string(taskToken)
	env.postCallback(func() {
		activityHandle, ok := env.scheduledActivities[activityID]
		if !ok {
			env.logger.Debug("CompleteActivity: ActivityID not found, could be already completed or cancelled.",
				zap.String(tagActivityID, activityID))
			return
		}
		request := convertActivityResultToRespondRequest("test-identity", taskToken, data, err)
		env.handleActivityResult(activityID, request, activityHandle.activityType)
	}, false /* do not auto schedule decision task, because activity might be still pending */)

	return nil
}

func (env *testWorkflowEnvironmentImpl) GetLogger() *zap.Logger {
	return env.logger
}

func (env *testWorkflowEnvironmentImpl) ExecuteActivity(parameters executeActivityParameters, callback resultHandler) *activityInfo {
	activityInfo := &activityInfo{fmt.Sprintf("%d", env.nextId())}

	task := newTestActivityTask(
		defaultTestWorkflowID,
		defaultTestRunID,
		activityInfo.activityID,
		parameters.ActivityType.Name,
		parameters.Input,
	)

	taskHandler := env.newTestActivityTaskHandler(parameters.TaskListName)
	activityHandle := &activityHandle{callback: callback, activityType: parameters.ActivityType.Name}
	env.scheduledActivities[activityInfo.activityID] = activityHandle
	env.runningActivityCount.Inc()

	// activity runs in separate goroutinue outside of workflow dispatcher
	go func() {
		result, err := taskHandler.Execute(task)
		if err != nil {
			panic(err)
		}
		// post activity result to workflow dispatcher
		env.postCallback(func() {
			env.handleActivityResult(activityInfo.activityID, result, parameters.ActivityType.Name)
		}, false /* do not auto schedule decision task, because activity might be still pending */)
		env.runningActivityCount.Dec()
	}()

	return activityInfo
}

func (env *testWorkflowEnvironmentImpl) handleActivityResult(activityID string, result interface{}, activityType string) {
	env.logger.Debug(fmt.Sprintf("handleActivityResult: %T.", result),
		zap.String(tagActivityID, activityID), zap.String(tagActivityType, activityType))
	if result == nil {
		// In case activity returns ErrActivityResultPending, the respond will be nil, and we don's need to do anything.
		// Activity will need to complete asynchronously using CompleteActivity().
		if env.onActivityEndedListener != nil {
			env.onActivityEndedListener(nil, ErrActivityResultPending, activityType)
		}
		return
	}

	// this is running in dispatcher
	activityHandle, ok := env.scheduledActivities[activityID]
	if !ok {
		env.logger.Debug("handleActivityResult: ActivityID not exists, could be already completed or cancelled.",
			zap.String(tagActivityID, activityID))
		return
	}

	delete(env.scheduledActivities, activityID)

	var blob []byte
	var err error

	switch request := result.(type) {
	case *shared.RespondActivityTaskCanceledRequest:
		err = NewCanceledError(request.Details)
		activityHandle.callback(nil, err)
	case *shared.RespondActivityTaskFailedRequest:
		err = NewErrorWithDetails(*request.Reason, request.Details)
		activityHandle.callback(nil, err)
	case *shared.RespondActivityTaskCompletedRequest:
		blob = request.Result_
		activityHandle.callback(blob, nil)
	default:
		panic(fmt.Sprintf("unsupported respond type %T", result))
	}

	if env.onActivityEndedListener != nil {
		env.onActivityEndedListener(EncodedValue(blob), err, activityType)
	}

	env.startDecisionTask()
}

// Execute executes the activity code. This is the wrapper where we call ActivityTaskStartedListener hook.
func (a *activityExecutorWrapper) Execute(ctx context.Context, input []byte) ([]byte, error) {
	if a.env.onActivityStartedListener != nil {
		a.env.postCallback(func() {
			a.env.onActivityStartedListener(ctx, EncodedValues(input))
		}, false)
	}
	return a.activity.Execute(ctx, input)
}

func (env *testWorkflowEnvironmentImpl) newTestActivityTaskHandler(taskList string) ActivityTaskHandler {
	wOptions := env.workerOptions.(*workerOptions)
	params := workerExecutionParameters{
		TaskList:     taskList,
		Identity:     wOptions.identity,
		MetricsScope: wOptions.metricsScope,
		Logger:       env.logger,
		UserContext:  wOptions.userContext,
	}
	ensureRequiredParams(&params)

	var activities []activity
	for fnName, tasklistActivity := range env.taskListSpecificActivities {
		if _, ok := tasklistActivity.taskLists[taskList]; ok {
			activities = append(activities, env.wrapActivity(&activityExecutor{name: fnName, fn: tasklistActivity.fn}))
		}
	}

	addActivities := func(registeredActivities []activity) {
		for _, a := range registeredActivities {
			fnName := a.ActivityType().Name
			if _, ok := env.taskListSpecificActivities[fnName]; ok {
				// activity is registered to a specific taskList, so ignore it from the global registered activities.
				continue
			}
			activities = append(activities, env.wrapActivity(a))
		}
	}

	addActivities(env.testSuite.hostEnv.getRegisteredActivities())
	addActivities(getHostEnvironment().getRegisteredActivities())

	if len(activities) == 0 {
		panic(fmt.Sprintf("no activity is registered for tasklist '%v'", taskList))
	}

	taskHandler := newActivityTaskHandler(activities, env.service, params)
	return taskHandler
}

func (env *testWorkflowEnvironmentImpl) wrapActivity(a activity) *activityExecutorWrapper {
	fnName := a.ActivityType().Name
	if overrideFn, ok := env.overrodeActivities[fnName]; ok {
		// override activity
		a = &activityExecutor{name: fnName, fn: overrideFn}
	}

	activityWrapper := &activityExecutorWrapper{activity: a, env: env}
	return activityWrapper
}

func newTestActivityTask(workflowID, runID, activityID, activityType string, input []byte) *shared.PollForActivityTaskResponse {
	task := &shared.PollForActivityTaskResponse{
		WorkflowExecution: &shared.WorkflowExecution{
			WorkflowId: common.StringPtr(workflowID),
			RunId:      common.StringPtr(runID),
		},
		ActivityId:   common.StringPtr(activityID),
		TaskToken:    []byte(activityID), // use activityID as TaskToken so we can map TaskToken in heartbeat calls.
		ActivityType: &shared.ActivityType{Name: common.StringPtr(activityType)},
		Input:        input,
	}
	return task
}

func (env *testWorkflowEnvironmentImpl) NewTimer(d time.Duration, callback resultHandler) *timerInfo {
	nextId := env.nextId()
	timerInfo := &timerInfo{fmt.Sprintf("%d", nextId)}
	timer := env.clock.AfterFunc(d, func() {
		env.postCallback(func() {
			delete(env.scheduledTimers, timerInfo.timerID)
			callback(nil, nil)
			if env.onTimerFiredListener != nil {
				env.onTimerFiredListener(timerInfo.timerID)
			}
		}, true)
	})
	env.scheduledTimers[timerInfo.timerID] = &timerHandle{timer: timer, callback: callback, duration: d, timerId: nextId}
	if env.onTimerScheduledListener != nil {
		env.onTimerScheduledListener(timerInfo.timerID, d)
	}
	return timerInfo
}

func (env *testWorkflowEnvironmentImpl) Now() time.Time {
	return env.clock.Now()
}

func (env *testWorkflowEnvironmentImpl) WorkflowInfo() *WorkflowInfo {
	return env.workflowInfo
}

func (env *testWorkflowEnvironmentImpl) RegisterCancel(handler func()) {
	env.workflowCancelHandler = handler
}

func (env *testWorkflowEnvironmentImpl) RequestCancelWorkflow(domainName, workflowID, runID string) error {
	env.workflowCancelHandler()
	return nil
}

func (env *testWorkflowEnvironmentImpl) nextId() int {
	activityID := env.counterID
	env.counterID++
	return activityID
}

// make sure interface is implemented
var _ workflowEnvironment = (*testWorkflowEnvironmentImpl)(nil)