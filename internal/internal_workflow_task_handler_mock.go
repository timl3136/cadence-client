// Code generated by mockery v2.53.3. DO NOT EDIT.

package internal

import mock "github.com/stretchr/testify/mock"

// MockWorkflowTaskHandler is an autogenerated mock type for the WorkflowTaskHandler type
type MockWorkflowTaskHandler struct {
	mock.Mock
}

type MockWorkflowTaskHandler_Expecter struct {
	mock *mock.Mock
}

func (_m *MockWorkflowTaskHandler) EXPECT() *MockWorkflowTaskHandler_Expecter {
	return &MockWorkflowTaskHandler_Expecter{mock: &_m.Mock}
}

// ProcessWorkflowTask provides a mock function with given fields: task, f
func (_m *MockWorkflowTaskHandler) ProcessWorkflowTask(task *workflowTask, f decisionHeartbeatFunc) (interface{}, error) {
	ret := _m.Called(task, f)

	if len(ret) == 0 {
		panic("no return value specified for ProcessWorkflowTask")
	}

	var r0 interface{}
	var r1 error
	if rf, ok := ret.Get(0).(func(*workflowTask, decisionHeartbeatFunc) (interface{}, error)); ok {
		return rf(task, f)
	}
	if rf, ok := ret.Get(0).(func(*workflowTask, decisionHeartbeatFunc) interface{}); ok {
		r0 = rf(task, f)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(interface{})
		}
	}

	if rf, ok := ret.Get(1).(func(*workflowTask, decisionHeartbeatFunc) error); ok {
		r1 = rf(task, f)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// MockWorkflowTaskHandler_ProcessWorkflowTask_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'ProcessWorkflowTask'
type MockWorkflowTaskHandler_ProcessWorkflowTask_Call struct {
	*mock.Call
}

// ProcessWorkflowTask is a helper method to define mock.On call
//   - task *workflowTask
//   - f decisionHeartbeatFunc
func (_e *MockWorkflowTaskHandler_Expecter) ProcessWorkflowTask(task interface{}, f interface{}) *MockWorkflowTaskHandler_ProcessWorkflowTask_Call {
	return &MockWorkflowTaskHandler_ProcessWorkflowTask_Call{Call: _e.mock.On("ProcessWorkflowTask", task, f)}
}

func (_c *MockWorkflowTaskHandler_ProcessWorkflowTask_Call) Run(run func(task *workflowTask, f decisionHeartbeatFunc)) *MockWorkflowTaskHandler_ProcessWorkflowTask_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run(args[0].(*workflowTask), args[1].(decisionHeartbeatFunc))
	})
	return _c
}

func (_c *MockWorkflowTaskHandler_ProcessWorkflowTask_Call) Return(response interface{}, err error) *MockWorkflowTaskHandler_ProcessWorkflowTask_Call {
	_c.Call.Return(response, err)
	return _c
}

func (_c *MockWorkflowTaskHandler_ProcessWorkflowTask_Call) RunAndReturn(run func(*workflowTask, decisionHeartbeatFunc) (interface{}, error)) *MockWorkflowTaskHandler_ProcessWorkflowTask_Call {
	_c.Call.Return(run)
	return _c
}

// NewMockWorkflowTaskHandler creates a new instance of MockWorkflowTaskHandler. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
// The first argument is typically a *testing.T value.
func NewMockWorkflowTaskHandler(t interface {
	mock.TestingT
	Cleanup(func())
}) *MockWorkflowTaskHandler {
	mock := &MockWorkflowTaskHandler{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}
