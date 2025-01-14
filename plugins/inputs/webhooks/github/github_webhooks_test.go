package github

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/testutil"
)

func GithubWebhookRequest(t *testing.T, event, jsonString string) {
	var acc testutil.Accumulator
	gh := &GithubWebhook{Path: "/github", acc: &acc, log: testutil.Logger{}}
	req, err := http.NewRequest("POST", "/github", strings.NewReader(jsonString))
	require.NoError(t, err)
	req.Header.Add("X-Github-Event", event)
	w := httptest.NewRecorder()
	gh.eventHandler(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("POST "+event+" returned HTTP status code %v.\nExpected %v", w.Code, http.StatusOK)
	}
}

func GithubWebhookRequestWithSignature(t *testing.T, event, jsonString, signature string, expectedStatus int) {
	var acc testutil.Accumulator
	gh := &GithubWebhook{Path: "/github", Secret: "signature", acc: &acc, log: testutil.Logger{}}
	req, err := http.NewRequest("POST", "/github", strings.NewReader(jsonString))
	require.NoError(t, err)
	req.Header.Add("X-Github-Event", event)
	req.Header.Add("X-Hub-Signature", signature)
	w := httptest.NewRecorder()
	gh.eventHandler(w, req)
	if w.Code != expectedStatus {
		t.Errorf("POST "+event+" returned HTTP status code %v.\nExpected %v", w.Code, expectedStatus)
	}
}

func TestCommitCommentEvent(t *testing.T) {
	GithubWebhookRequest(t, "commit_comment", CommitCommentEventJSON())
}

func TestPingEvent(t *testing.T) {
	GithubWebhookRequest(t, "ping", "")
}

func TestDeleteEvent(t *testing.T) {
	GithubWebhookRequest(t, "delete", DeleteEventJSON())
}

func TestDeploymentEvent(t *testing.T) {
	GithubWebhookRequest(t, "deployment", DeploymentEventJSON())
}

func TestDeploymentStatusEvent(t *testing.T) {
	GithubWebhookRequest(t, "deployment_status", DeploymentStatusEventJSON())
}

func TestForkEvent(t *testing.T) {
	GithubWebhookRequest(t, "fork", ForkEventJSON())
}

func TestGollumEvent(t *testing.T) {
	GithubWebhookRequest(t, "gollum", GollumEventJSON())
}

func TestIssueCommentEvent(t *testing.T) {
	GithubWebhookRequest(t, "issue_comment", IssueCommentEventJSON())
}

func TestIssuesEvent(t *testing.T) {
	GithubWebhookRequest(t, "issues", IssuesEventJSON())
}

func TestMemberEvent(t *testing.T) {
	GithubWebhookRequest(t, "member", MemberEventJSON())
}

func TestMembershipEvent(t *testing.T) {
	GithubWebhookRequest(t, "membership", MembershipEventJSON())
}

func TestPageBuildEvent(t *testing.T) {
	GithubWebhookRequest(t, "page_build", PageBuildEventJSON())
}

func TestPublicEvent(t *testing.T) {
	GithubWebhookRequest(t, "public", PublicEventJSON())
}

func TestPullRequestReviewCommentEvent(t *testing.T) {
	GithubWebhookRequest(t, "pull_request_review_comment", PullRequestReviewCommentEventJSON())
}

func TestPushEvent(t *testing.T) {
	GithubWebhookRequest(t, "push", PushEventJSON())
}

func TestReleaseEvent(t *testing.T) {
	GithubWebhookRequest(t, "release", ReleaseEventJSON())
}

func TestRepositoryEvent(t *testing.T) {
	GithubWebhookRequest(t, "repository", RepositoryEventJSON())
}

func TestStatusEvent(t *testing.T) {
	GithubWebhookRequest(t, "status", StatusEventJSON())
}

func TestTeamAddEvent(t *testing.T) {
	GithubWebhookRequest(t, "team_add", TeamAddEventJSON())
}

func TestWatchEvent(t *testing.T) {
	GithubWebhookRequest(t, "watch", WatchEventJSON())
}

func TestEventWithSignatureFail(t *testing.T) {
	GithubWebhookRequestWithSignature(t, "watch", WatchEventJSON(), "signature", http.StatusBadRequest)
}

func TestEventWithSignatureSuccess(t *testing.T) {
	GithubWebhookRequestWithSignature(t, "watch", WatchEventJSON(), generateSignature("signature", []byte(WatchEventJSON())), http.StatusOK)
}

func TestWorkflowJob(t *testing.T) {
	GithubWebhookRequest(t, "workflow_job", WorkflowJobJSON())
}

func TestWorkflowRun(t *testing.T) {
	GithubWebhookRequest(t, "workflow_run", WorkflowRunJSON())
}

func TestCheckSignatureSuccess(t *testing.T) {
	if !checkSignature("my_little_secret", []byte("random-signature-body"), "sha1=3dca279e731c97c38e3019a075dee9ebbd0a99f0") {
		t.Errorf("check signature failed")
	}
}

func TestCheckSignatureFailed(t *testing.T) {
	if checkSignature("m_little_secret", []byte("random-signature-body"), "sha1=3dca279e731c97c38e3019a075dee9ebbd0a99f0") {
		t.Errorf("check signature failed")
	}
}
