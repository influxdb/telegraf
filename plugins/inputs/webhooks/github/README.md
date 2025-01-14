# github webhooks

You should configure your Organization's Webhooks to point at the `webhooks`
service. To do this go to `github.com/{my_organization}` and click
`Settings > Webhooks > Add webhook`. In the resulting menu set `Payload URL` to
`http://<my_ip>:1619/github`, `Content type` to `application/json` and under
the section `Which events would you like to trigger this webhook?` select
'Send me **everything**'. By default all of the events will write to the
`github_webhooks` measurement, this is configurable by setting the
`measurement_name` in the config file.

You can also add a secret that will be used by telegraf to verify the
authenticity of the requests.

## Metrics

The titles of the following sections are links to the full payloads and details
for each event. The body contains what information from the event is persisted.
The format is as follows:

```toml
# TAGS
* 'tagKey' = `tagValue` type
# FIELDS
* 'fieldKey' = `fieldValue` type
```

The tag values and field values show the place on the incoming JSON object
where the data is sourced from.

### [`commit_comment` event](https://docs.github.com/en/webhooks/webhook-events-and-payloads#commit_comment)

**Tags:**

* 'event' = `headers[X-Github-Event]` string
* 'repository' = `event.repository.full_name` string
* 'private' = `event.repository.private` bool
* 'user' = `event.sender.login` string
* 'admin' = `event.sender.site_admin` bool

**Fields:**

* 'stars' = `event.repository.stargazers_count` int
* 'forks' = `event.repository.forks_count` int
* 'issues' = `event.repository.open_issues_count` int
* 'commit' = `event.comment.commit_id` string
* 'comment' = `event.comment.body` string

### [`create` event](https://docs.github.com/en/webhooks/webhook-events-and-payloads#create)

**Tags:**

* 'event' = `headers[X-Github-Event]` string
* 'repository' = `event.repository.full_name` string
* 'private' = `event.repository.private` bool
* 'user' = `event.sender.login` string
* 'admin' = `event.sender.site_admin` bool

**Fields:**

* 'stars' = `event.repository.stargazers_count` int
* 'forks' = `event.repository.forks_count` int
* 'issues' = `event.repository.open_issues_count` int
* 'ref' = `event.ref` string
* 'refType' = `event.ref_type` string

### [`delete` event](https://docs.github.com/en/webhooks/webhook-events-and-payloads#delete)

**Tags:**

* 'event' = `headers[X-Github-Event]` string
* 'repository' = `event.repository.full_name` string
* 'private' = `event.repository.private` bool
* 'user' = `event.sender.login` string
* 'admin' = `event.sender.site_admin` bool

**Fields:**

* 'stars' = `event.repository.stargazers_count` int
* 'forks' = `event.repository.forks_count` int
* 'issues' = `event.repository.open_issues_count` int
* 'ref' = `event.ref` string
* 'refType' = `event.ref_type` string

### [`deployment` event](https://docs.github.com/en/webhooks/webhook-events-and-payloads#deployment)

**Tags:**

* 'event' = `headers[X-Github-Event]` string
* 'repository' = `event.repository.full_name` string
* 'private' = `event.repository.private` bool
* 'user' = `event.sender.login` string
* 'admin' = `event.sender.site_admin` bool

**Fields:**

* 'stars' = `event.repository.stargazers_count` int
* 'forks' = `event.repository.forks_count` int
* 'issues' = `event.repository.open_issues_count` int
* 'commit' = `event.deployment.sha` string
* 'task' = `event.deployment.task` string
* 'environment' = `event.deployment.environment` string
* 'description' = `event.deployment.description` string

### [`deployment_status` event](https://docs.github.com/en/webhooks/webhook-events-and-payloads#deployment_status)

**Tags:**

* 'event' = `headers[X-Github-Event]` string
* 'repository' = `event.repository.full_name` string
* 'private' = `event.repository.private` bool
* 'user' = `event.sender.login` string
* 'admin' = `event.sender.site_admin` bool

**Fields:**

* 'stars' = `event.repository.stargazers_count` int
* 'forks' = `event.repository.forks_count` int
* 'issues' = `event.repository.open_issues_count` int
* 'commit' = `event.deployment.sha` string
* 'task' = `event.deployment.task` string
* 'environment' = `event.deployment.environment` string
* 'description' = `event.deployment.description` string
* 'depState' = `event.deployment_status.state` string
* 'depDescription' = `event.deployment_status.description` string

### [`fork` event](https://docs.github.com/en/webhooks/webhook-events-and-payloads#fork)

**Tags:**

* 'event' = `headers[X-Github-Event]` string
* 'repository' = `event.repository.full_name` string
* 'private' = `event.repository.private` bool
* 'user' = `event.sender.login` string
* 'admin' = `event.sender.site_admin` bool

**Fields:**

* 'stars' = `event.repository.stargazers_count` int
* 'forks' = `event.repository.forks_count` int
* 'issues' = `event.repository.open_issues_count` int
* 'forkee' = `event.forkee.repository` string

### [`gollum` event](https://docs.github.com/en/webhooks/webhook-events-and-payloads#gollum)

**Tags:**

* 'event' = `headers[X-Github-Event]` string
* 'repository' = `event.repository.full_name` string
* 'private' = `event.repository.private` bool
* 'user' = `event.sender.login` string
* 'admin' = `event.sender.site_admin` bool

**Fields:**

* 'stars' = `event.repository.stargazers_count` int
* 'forks' = `event.repository.forks_count` int
* 'issues' = `event.repository.open_issues_count` int

### [`issue_comment` event](https://docs.github.com/en/webhooks/webhook-events-and-payloads#issue_comment)

**Tags:**

* 'event' = `headers[X-Github-Event]` string
* 'repository' = `event.repository.full_name` string
* 'private' = `event.repository.private` bool
* 'user' = `event.sender.login` string
* 'admin' = `event.sender.site_admin` bool
* 'issue' = `event.issue.number` int

**Fields:**

* 'stars' = `event.repository.stargazers_count` int
* 'forks' = `event.repository.forks_count` int
* 'issues' = `event.repository.open_issues_count` int
* 'title' = `event.issue.title` string
* 'comments' = `event.issue.comments` int
* 'body' = `event.comment.body` string

### [`issues` event](https://docs.github.com/en/webhooks/webhook-events-and-payloads#issues)

**Tags:**

* 'event' = `headers[X-Github-Event]` string
* 'repository' = `event.repository.full_name` string
* 'private' = `event.repository.private` bool
* 'user' = `event.sender.login` string
* 'admin' = `event.sender.site_admin` bool
* 'issue' = `event.issue.number` int
* 'action' = `event.action` string

**Fields:**

* 'stars' = `event.repository.stargazers_count` int
* 'forks' = `event.repository.forks_count` int
* 'issues' = `event.repository.open_issues_count` int
* 'title' = `event.issue.title` string
* 'comments' = `event.issue.comments` int

### [`member` event](https://docs.github.com/en/webhooks/webhook-events-and-payloads#member)

**Tags:**

* 'event' = `headers[X-Github-Event]` string
* 'repository' = `event.repository.full_name` string
* 'private' = `event.repository.private` bool
* 'user' = `event.sender.login` string
* 'admin' = `event.sender.site_admin` bool

**Fields:**

* 'stars' = `event.repository.stargazers_count` int
* 'forks' = `event.repository.forks_count` int
* 'issues' = `event.repository.open_issues_count` int
* 'newMember' = `event.sender.login` string
* 'newMemberStatus' = `event.sender.site_admin` bool

### [`membership` event](https://docs.github.com/en/webhooks/webhook-events-and-payloads#membership)

**Tags:**

* 'event' = `headers[X-Github-Event]` string
* 'user' = `event.sender.login` string
* 'admin' = `event.sender.site_admin` bool
* 'action' = `event.action` string

**Fields:**

* 'newMember' = `event.sender.login` string
* 'newMemberStatus' = `event.sender.site_admin` bool

### [`page_build` event](https://docs.github.com/en/webhooks/webhook-events-and-payloads#page_build)

**Tags:**

* 'event' = `headers[X-Github-Event]` string
* 'repository' = `event.repository.full_name` string
* 'private' = `event.repository.private` bool
* 'user' = `event.sender.login` string
* 'admin' = `event.sender.site_admin` bool

**Fields:**

* 'stars' = `event.repository.stargazers_count` int
* 'forks' = `event.repository.forks_count` int
* 'issues' = `event.repository.open_issues_count` int

### [`public` event](https://docs.github.com/en/webhooks/webhook-events-and-payloads#public)

**Tags:**

* 'event' = `headers[X-Github-Event]` string
* 'repository' = `event.repository.full_name` string
* 'private' = `event.repository.private` bool
* 'user' = `event.sender.login` string
* 'admin' = `event.sender.site_admin` bool

**Fields:**

* 'stars' = `event.repository.stargazers_count` int
* 'forks' = `event.repository.forks_count` int
* 'issues' = `event.repository.open_issues_count` int

### [`pull_request_review_comment` event](https://docs.github.com/en/webhooks/webhook-events-and-payloads#pull_request_review_comment)

**Tags:**

* 'event' = `headers[X-Github-Event]` string
* 'action' = `event.action` string
* 'repository' = `event.repository.full_name` string
* 'private' = `event.repository.private` bool
* 'user' = `event.sender.login` string
* 'admin' = `event.sender.site_admin` bool
* 'prNumber' = `event.pull_request.number` int

**Fields:**

* 'stars' = `event.repository.stargazers_count` int
* 'forks' = `event.repository.forks_count` int
* 'issues' = `event.repository.open_issues_count` int
* 'state' = `event.pull_request.state` string
* 'title' = `event.pull_request.title` string
* 'comments' = `event.pull_request.comments` int
* 'commits' = `event.pull_request.commits` int
* 'additions' = `event.pull_request.additions` int
* 'deletions' = `event.pull_request.deletions` int
* 'changedFiles' = `event.pull_request.changed_files` int
* 'commentFile' = `event.comment.file` string
* 'comment' = `event.comment.body` string

### [`pull_request` event](https://docs.github.com/en/webhooks/webhook-events-and-payloads#pull_request)

**Tags:**

* 'event' = `headers[X-Github-Event]` string
* 'action' = `event.action` string
* 'repository' = `event.repository.full_name` string
* 'private' = `event.repository.private` bool
* 'user' = `event.sender.login` string
* 'admin' = `event.sender.site_admin` bool
* 'prNumber' = `event.pull_request.number` int

**Fields:**

* 'stars' = `event.repository.stargazers_count` int
* 'forks' = `event.repository.forks_count` int
* 'issues' = `event.repository.open_issues_count` int
* 'state' = `event.pull_request.state` string
* 'title' = `event.pull_request.title` string
* 'comments' = `event.pull_request.comments` int
* 'commits' = `event.pull_request.commits` int
* 'additions' = `event.pull_request.additions` int
* 'deletions' = `event.pull_request.deletions` int
* 'changedFiles' = `event.pull_request.changed_files` int

### [`push` event](https://docs.github.com/en/webhooks/webhook-events-and-payloads#push)

**Tags:**

* 'event' = `headers[X-Github-Event]` string
* 'repository' = `event.repository.full_name` string
* 'private' = `event.repository.private` bool
* 'user' = `event.sender.login` string
* 'admin' = `event.sender.site_admin` bool

**Fields:**

* 'stars' = `event.repository.stargazers_count` int
* 'forks' = `event.repository.forks_count` int
* 'issues' = `event.repository.open_issues_count` int
* 'ref' = `event.ref` string
* 'before' = `event.before` string
* 'after' = `event.after` string

### [`repository` event](https://docs.github.com/en/webhooks/webhook-events-and-payloads#repository)

**Tags:**

* 'event' = `headers[X-Github-Event]` string
* 'repository' = `event.repository.full_name` string
* 'private' = `event.repository.private` bool
* 'user' = `event.sender.login` string
* 'admin' = `event.sender.site_admin` bool

**Fields:**

* 'stars' = `event.repository.stargazers_count` int
* 'forks' = `event.repository.forks_count` int
* 'issues' = `event.repository.open_issues_count` int

### [`release` event](https://docs.github.com/en/webhooks/webhook-events-and-payloads#release)

**Tags:**

* 'event' = `headers[X-Github-Event]` string
* 'repository' = `event.repository.full_name` string
* 'private' = `event.repository.private` bool
* 'user' = `event.sender.login` string
* 'admin' = `event.sender.site_admin` bool

**Fields:**

* 'stars' = `event.repository.stargazers_count` int
* 'forks' = `event.repository.forks_count` int
* 'issues' = `event.repository.open_issues_count` int
* 'tagName' = `event.release.tag_name` string

### [`status` event](https://docs.github.com/en/webhooks/webhook-events-and-payloads#status)

**Tags:**

* 'event' = `headers[X-Github-Event]` string
* 'repository' = `event.repository.full_name` string
* 'private' = `event.repository.private` bool
* 'user' = `event.sender.login` string
* 'admin' = `event.sender.site_admin` bool

**Fields:**

* 'stars' = `event.repository.stargazers_count` int
* 'forks' = `event.repository.forks_count` int
* 'issues' = `event.repository.open_issues_count` int
* 'commit' = `event.sha` string
* 'state' = `event.state` string

### [`team_add` event](https://docs.github.com/en/webhooks/webhook-events-and-payloads#team_add)

**Tags:**

* 'event' = `headers[X-Github-Event]` string
* 'repository' = `event.repository.full_name` string
* 'private' = `event.repository.private` bool
* 'user' = `event.sender.login` string
* 'admin' = `event.sender.site_admin` bool

**Fields:**

* 'stars' = `event.repository.stargazers_count` int
* 'forks' = `event.repository.forks_count` int
* 'issues' = `event.repository.open_issues_count` int
* 'teamName' = `event.team.name` string

### [`watch` event](https://docs.github.com/en/webhooks/webhook-events-and-payloads#watch)

**Tags:**

* 'event' = `headers[X-Github-Event]` string
* 'repository' = `event.repository.full_name` string
* 'private' = `event.repository.private` bool
* 'user' = `event.sender.login` string
* 'admin' = `event.sender.site_admin` bool

**Fields:**

* 'stars' = `event.repository.stargazers_count` int
* 'forks' = `event.repository.forks_count` int
* 'issues' = `event.repository.open_issues_count` int

### [`workflow_job` event](https://docs.github.com/en/webhooks/webhook-events-and-payloads#workflow_job)

**Tags:**

* 'event' = `headers[X-Github-Event]` string
* 'action' = `event.action` string
* 'repository' = `event.repository.full_name` string
* 'private' = `event.repository.private` bool
* 'user' = `event.sender.login` string
* 'admin' = `event.sender.site_admin` bool
* 'name' = `event.workflow_job.name` string
* 'conclusion' = `event.workflow_job.conclusion` string

**Fields:**

* 'run_attempt' = `event.workflow_job.run_attempt` int
* 'queue_time' = `event.workflow_job.started_at - event.workflow_job.created_at at event.action = in_progress in milliseconds` int
* 'run_time' = `event.workflow_job.completed_at - event.workflow_job.started_at at event.action = completed in milliseconds` int
* 'head_branch' = `event.workflow_job.head_branch` string

### [`workflow_run` event](https://docs.github.com/en/webhooks/webhook-events-and-payloads#workflow_run)

**Tags:**

* 'event' = `headers[X-Github-Event]` string
* 'action' = `event.action` string
* 'repository' = `event.repository.full_name` string
* 'private' = `event.repository.private` bool
* 'user' = `event.sender.login` string
* 'admin' = `event.sender.site_admin` bool
* 'name' = `event.workflow_run.name` string
* 'conclusion' = `event.workflow_run.conclusion` string

**Fields:**

* 'run_attempt' = `event.workflow_run.run_attempt` int
* 'run_time' = `event.workflow_run.completed_at - event.workflow_run.run_started_at at event.action = completed in milliseconds` int
* 'head_branch' = `event.workflow_run.head_branch` string
