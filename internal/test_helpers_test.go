// Copyright (c) 2017-2021 Uber Technologies Inc.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package internal

import (
	"testing"

	"go.uber.org/cadence/internal/common/testlogger"

	"github.com/golang/mock/gomock"
)

// A collection of cross-test helpers.
// When a function is not intended to be used outside a file / suite, consider an instance method or variable instead.

// Creates a new workflow environment with the correct logger / testing.T configured.
func newTestWorkflowEnv(t *testing.T) *TestWorkflowEnvironment {
	s := WorkflowTestSuite{}
	s.SetLogger(testlogger.NewZap(t))
	// tally is not set since metrics are not noisy by default, and the test-instance
	// is largely useless without access to the instance for snapshots.
	env := s.NewTestWorkflowEnvironment()
	env.Test(t)
	return env
}

// this is the mock for yarpcCallOptions, as gomock requires the num of arguments to be the same.
// see getYarpcCallOptions for the default case.
func callOptions() []interface{} {
	return []interface{}{
		gomock.Any(), // library version
		gomock.Any(), // feature version
		gomock.Any(), // client name
		gomock.Any(), // feature flags
	}
}

// this is the mock for yarpcCallOptions, as gomock requires the num of arguments to be the same.
// see getYarpcCallOptions for the default case.
func callOptionsWithIsolationGroupHeader() []interface{} {
	return []interface{}{
		gomock.Any(), // library version
		gomock.Any(), // feature version
		gomock.Any(), // client name
		gomock.Any(), // feature flags
		gomock.Any(), // isolation group header
	}
}
