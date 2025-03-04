// Copyright 2014-2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package action_test

import (
	"bytes"
	"errors"
	"strings"
	"time"

	"github.com/juju/cmd/cmdtesting"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/apiserver/common"
	"github.com/juju/juju/apiserver/params"
	"github.com/juju/juju/cmd/juju/action"
)

type ShowOutputSuite struct {
	BaseActionSuite
}

var _ = gc.Suite(&ShowOutputSuite{})

func (s *ShowOutputSuite) SetUpTest(c *gc.C) {
	s.BaseActionSuite.SetUpTest(c)
}

func (s *ShowOutputSuite) TestInit(c *gc.C) {
	tests := []struct {
		should      string
		args        []string
		expectError string
	}{{
		should:      "fail with missing arg",
		args:        []string{},
		expectError: "no action ID specified",
	}, {
		should:      "fail with multiple args",
		args:        []string{"12345", "54321"},
		expectError: `unrecognized args: \["54321"\]`,
	}}

	for i, t := range tests {
		for _, modelFlag := range s.modelFlags {
			c.Logf("test %d: it should %s: juju show-action-output %s", i,
				t.should, strings.Join(t.args, " "))
			cmd, _ := action.NewShowOutputCommandForTest(s.store)
			args := append([]string{modelFlag, "admin"}, t.args...)
			err := cmdtesting.InitCommand(cmd, args)
			if t.expectError != "" {
				c.Check(err, gc.ErrorMatches, t.expectError)
			}
		}
	}
}

func (s *ShowOutputSuite) TestRun(c *gc.C) {
	tests := []struct {
		should            string
		withClientWait    string
		withClientQueryID string
		withAPIDelay      time.Duration
		withAPITimeout    time.Duration
		withTags          params.FindTagsResults
		withAPIResponse   []params.ActionResult
		withAPIError      string
		expectedErr       string
		expectedOutput    string
	}{{
		should:         "handle wait-time formatting errors",
		withClientWait: "not-a-duration-at-all",
		expectedErr:    "time: invalid duration not-a-duration-at-all",
	}, {
		should:            "timeout if result never comes",
		withClientWait:    "3s",
		withAPIDelay:      6 * time.Second,
		withAPITimeout:    10 * time.Second,
		withClientQueryID: validActionId,
		withTags:          tagsForIdPrefix(validActionId, validActionTagString),
		withAPIResponse:   []params.ActionResult{{}},
		expectedErr:       "timeout reached",
		expectedOutput: `
status: pending
timing:
  enqueued: 2015-02-14 08:13:00 +0000 UTC
  started: 2015-02-14 08:15:00 +0000 UTC
`[1:],
	}, {
		should:            "pass api error through properly",
		withClientQueryID: validActionId,
		withAPITimeout:    10 * time.Second,
		withTags:          tagsForIdPrefix(validActionId, validActionTagString),
		withAPIError:      "api call error",
		expectedErr:       "api call error",
	}, {
		should:            "fail with no tag matches",
		withClientQueryID: validActionId,
		withAPITimeout:    10 * time.Second,
		withTags:          tagsForIdPrefix(validActionId),
		expectedErr:       `actions for identifier "` + validActionId + `" not found`,
	}, {
		should:            "fail with no results",
		withClientQueryID: validActionId,
		withAPITimeout:    10 * time.Second,
		withTags:          tagsForIdPrefix(validActionId, validActionTagString),
		withAPIResponse:   []params.ActionResult{},
		expectedErr:       "no results for action " + validActionId,
	}, {
		should:            "error correctly with multiple results",
		withClientQueryID: validActionId,
		withAPITimeout:    10 * time.Second,
		withTags:          tagsForIdPrefix(validActionId, validActionTagString),
		withAPIResponse:   []params.ActionResult{{}, {}},
		expectedErr:       "too many results for action " + validActionId,
	}, {
		should:            "pass through an error from the API server",
		withClientQueryID: validActionId,
		withAPITimeout:    10 * time.Second,
		withTags:          tagsForIdPrefix(validActionId, validActionTagString),
		withAPIResponse: []params.ActionResult{{
			Error: common.ServerError(errors.New("an apiserver error")),
		}},
		expectedErr: "an apiserver error",
	}, {
		should:            "only return once status is no longer running or pending",
		withAPIDelay:      1 * time.Second,
		withClientWait:    "30s",
		withClientQueryID: validActionId,
		withAPITimeout:    3 * time.Second,
		withTags:          tagsForIdPrefix(validActionId, validActionTagString),
		withAPIResponse: []params.ActionResult{{
			Status: "running",
			Output: map[string]interface{}{
				"foo": map[string]interface{}{
					"bar": "baz",
				},
			},
			Enqueued: time.Date(2015, time.February, 14, 8, 13, 0, 0, time.UTC),
			Started:  time.Date(2015, time.February, 14, 8, 15, 0, 0, time.UTC),
		}},
		expectedErr: "test timed out before wait time",
	}, {
		should:            "pretty-print action output",
		withClientQueryID: validActionId,
		withAPITimeout:    10 * time.Second,
		withTags:          tagsForIdPrefix(validActionId, validActionTagString),
		withAPIResponse: []params.ActionResult{{
			Status:  "complete",
			Message: "oh dear",
			Output: map[string]interface{}{
				"foo": map[string]interface{}{
					"bar": "baz",
				},
			},
			Enqueued:  time.Date(2015, time.February, 14, 8, 13, 0, 0, time.UTC),
			Started:   time.Date(2015, time.February, 14, 8, 15, 0, 0, time.UTC),
			Completed: time.Date(2015, time.February, 14, 8, 15, 30, 0, time.UTC),
		}},
		expectedOutput: `
message: oh dear
results:
  foo:
    bar: baz
status: complete
timing:
  completed: 2015-02-14 08:15:30 +0000 UTC
  enqueued: 2015-02-14 08:13:00 +0000 UTC
  started: 2015-02-14 08:15:00 +0000 UTC
`[1:],
	}, {
		should:            "pretty-print action output with no completed time",
		withClientQueryID: validActionId,
		withAPITimeout:    10 * time.Second,
		withTags:          tagsForIdPrefix(validActionId, validActionTagString),
		withAPIResponse: []params.ActionResult{{
			Status: "pending",
			Output: map[string]interface{}{
				"foo": map[string]interface{}{
					"bar": "baz",
				},
			},
			Enqueued: time.Date(2015, time.February, 14, 8, 13, 0, 0, time.UTC),
			Started:  time.Date(2015, time.February, 14, 8, 15, 0, 0, time.UTC),
		}},
		expectedErr: "timeout reached",
		expectedOutput: `
results:
  foo:
    bar: baz
status: pending
timing:
  enqueued: 2015-02-14 08:13:00 +0000 UTC
  started: 2015-02-14 08:15:00 +0000 UTC
`[1:],
	}, {
		should:            "pretty-print action output with no enqueued time",
		withClientQueryID: validActionId,
		withAPITimeout:    10 * time.Second,
		withTags:          tagsForIdPrefix(validActionId, validActionTagString),
		withAPIResponse: []params.ActionResult{{
			Status: "pending",
			Output: map[string]interface{}{
				"foo": map[string]interface{}{
					"bar": "baz",
				},
			},
			Completed: time.Date(2015, time.February, 14, 8, 15, 30, 0, time.UTC),
			Started:   time.Date(2015, time.February, 14, 8, 15, 0, 0, time.UTC),
		}},
		expectedErr: "timeout reached",
		expectedOutput: `
results:
  foo:
    bar: baz
status: pending
timing:
  completed: 2015-02-14 08:15:30 +0000 UTC
  started: 2015-02-14 08:15:00 +0000 UTC
`[1:],
	}, {
		should:            "pretty-print action output with no started time",
		withClientQueryID: validActionId,
		withAPITimeout:    10 * time.Second,
		withTags:          tagsForIdPrefix(validActionId, validActionTagString),
		withAPIResponse: []params.ActionResult{{
			Status: "pending",
			Output: map[string]interface{}{
				"foo": map[string]interface{}{
					"bar": "baz",
				},
			},
			Enqueued:  time.Date(2015, time.February, 14, 8, 13, 0, 0, time.UTC),
			Completed: time.Date(2015, time.February, 14, 8, 15, 30, 0, time.UTC),
		}},
		expectedErr: "timeout reached",
		expectedOutput: `
results:
  foo:
    bar: baz
status: pending
timing:
  completed: 2015-02-14 08:15:30 +0000 UTC
  enqueued: 2015-02-14 08:13:00 +0000 UTC
`[1:],
	}, {
		should:            "set an appropriate timer and wait, get a result",
		withClientQueryID: validActionId,
		withAPITimeout:    10 * time.Second,
		withClientWait:    "4s",
		withAPIDelay:      2 * time.Second,
		withTags:          tagsForIdPrefix(validActionId, validActionTagString),
		withAPIResponse: []params.ActionResult{{
			Status: "completed",
			Output: map[string]interface{}{
				"foo": map[string]interface{}{
					"bar": "baz",
				},
			},
			Enqueued:  time.Date(2015, time.February, 14, 8, 13, 0, 0, time.UTC),
			Completed: time.Date(2015, time.February, 14, 8, 15, 30, 0, time.UTC),
		}},
		expectedOutput: `
results:
  foo:
    bar: baz
status: completed
timing:
  completed: 2015-02-14 08:15:30 +0000 UTC
  enqueued: 2015-02-14 08:13:00 +0000 UTC
`[1:],
	}}

	for i, t := range tests {
		for _, modelFlag := range s.modelFlags {
			c.Logf("test %d (model option %v): should %s", i, modelFlag, t.should)
			testRunHelper(
				c, s,
				makeFakeClient(
					t.withAPIDelay,
					t.withAPITimeout,
					t.withTags,
					t.withAPIResponse,
					params.ActionsByNames{},
					t.withAPIError),
				t.expectedErr,
				t.expectedOutput,
				t.withClientWait,
				t.withClientQueryID,
				modelFlag,
			)
		}
	}
}

func testRunHelper(c *gc.C, s *ShowOutputSuite, client *fakeAPIClient, expectedErr, expectedOutput, wait, query, modelFlag string) {
	unpatch := s.BaseActionSuite.patchAPIClient(client)
	defer unpatch()
	args := append([]string{modelFlag, "admin"}, query)
	if wait != "" {
		args = append(args, "--wait", wait)
	}
	cmd, _ := action.NewShowOutputCommandForTest(s.store)
	ctx, err := cmdtesting.RunCommand(c, cmd, args...)
	if expectedErr != "" {
		c.Check(err, gc.ErrorMatches, expectedErr)
	} else {
		c.Assert(err, gc.IsNil)
		c.Check(ctx.Stdout.(*bytes.Buffer).String(), gc.Equals, expectedOutput)
	}
}

func makeFakeClient(
	delay, timeout time.Duration,
	tags params.FindTagsResults,
	response []params.ActionResult,
	actionsByNames params.ActionsByNames,
	errStr string,
) *fakeAPIClient {
	var delayTimer *time.Timer
	if delay != 0 {
		delayTimer = time.NewTimer(delay)
	}
	client := &fakeAPIClient{
		delay:            delayTimer,
		timeout:          time.NewTimer(timeout),
		actionTagMatches: tags,
		actionResults:    response,
		actionsByNames:   actionsByNames,
	}
	if errStr != "" {
		client.apiErr = errors.New(errStr)
	}
	return client
}
