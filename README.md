# JIRA Webhook to Chat Bridge

This app receives JIRA webhooks and generates a new webhook to be sent to
f.e. Slack or Rocket.Chat. It allows to configure a webhook destination
per JIRA project.

## Configuration

Configuration is done in a config file `config.yaml`. Example:

```
general:
  ticket_url: https://myticketsystem.example.com/tickets/
  listen: :8081
  secret: ThisIsNotAGoodSecretAndItsMeantForTesting
projects:
  PROJECT1:
    webhook: https://rocket.chat/hooks/...
  PROJECT2:
    webhook: https://rocket.chat/hooks/...
    ticket_url: https://myjira.example.com/issues/
```

* `general.ticket_url`: Default URL to link issues to. The issue ID will
   be added to the URL, must end with `/`
* `general.listen`: IP and port to listen
* `general.secret`: Secret token which will be added to the incoming webhook URL
* `projects.`: Webhook destination and optionally ticket URL per JIRA project

## Usage

The incoming webhook URL needs to be configured in JIRA. URL endpoint, example:

`http://myjcn.example.com:8081/ThisIsNotAGoodSecretAndItsMeantForTesting/jira`

For each incoming webhook an outgoing webhook will be sent to the configured
destination per JIRA project. If an incoming webhook is for a project which
is not configured, it is ignored.

Other endpoints:

* `/healthz`: Application healthcheck
* `/metrics`: Prometheus metrics

## Building

There is a Dockerfile which builds an executable image:

```
docker build -t local/jcn:latest .
```

## Running

The Docker image can be executed like this:

```
docker run --rm -ti -v $(pwd)/config.yaml:/etc/jira-chat-notifier/config.yaml:ro local/jcn:latest
```

## TODO

* Support multiple outgoing webhook endpoints
* Deploy to production
* Test with Slack
* Go testing
