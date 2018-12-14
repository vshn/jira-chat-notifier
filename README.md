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
    - webhook: https://rocket.chat/hooks/...
  PROJECT2:
    - webhook: https://rocket.chat/hooks/...
      ticket_url: https://myjira.example.com/issues/
    - webhook: https://rocket.chat/hooks/...
      on_events: created
```

* `general.ticket_url`: Default URL to link issues to. The issue ID will
   be added to the URL, must end with `/`
* `general.listen`: IP and port to listen
* `general.secret`: Secret token which will be added to the incoming webhook URL.
  This can also be configured via ENV var `URL_SECRET`.
* `projects.`: Webhook destination and optionally ticket URL per JIRA project
  * `projects.<PROJECT>[].on_events`: Choose which events trigger this outgoing
    webhook. Supported: `updated`, `created`. If not specified, all events are
    sent. Can be combined, separated by comma.

Thanks to the amazing go configuration library
[spf13/viper](https://github.com/spf13/viper) configuration is automatically reloaded
when the configuration file changes. This even applies for changes to a ConfigMap when
deployed on Kubernetes / OpenShift / APPUiO.

## Usage

The incoming webhook URL needs to be configured in JIRA. URL endpoint, example:

`http://myjcn.example.com:8081/ThisIsNotAGoodSecretAndItsMeantForTesting/jira`

For each incoming webhook an outgoing webhook will be sent to the configured
destinations per JIRA project. If an incoming webhook is for a project which
is not configured, it is ignored.

Other endpoints:

* `/healthz`: Application healthcheck
* `/metrics`: Prometheus metrics

## Building

There is a Dockerfile which builds an executable image:

```
docker build -t local/jcn:latest .
```

## CI/CD Pipeline and Deployment

The Gitlab CI Pipeline automatically builds and deploys the application to APPUiO.
All images are stored in the Gitlab container registry. For a very small image the
`Dockerfile` makes use of multistage build. To leverage container build caching
the builder stage is pushed to the registry and referenced in subsequent builds.

Git branches:
* `dev`: Each push triggers a build and deployment to APPUiO. Image tag: `dev`
* `master`: Only a build is triggered. Image tag: `latest`
* `tags`: A release is built and the manuel deployment to production activated.
  Image tag: `$gittag`

The deployment artifact for deployment on APPUiO is stored under `/deploy` as an
OpenShift template and is processed using `oc process`. Some artifacts need to be
manually created and are not part of the CI pipeline:

* `ConfigMaps`: The app configuration is stored in a ConfigMap, the name depends
  on the stage: `vshn-jira-chat-notifier-$stage`
* `Secret`: Contains the URL secret which is attached to the Pod via ENV vars `vshn-jira-chat-notifier-$stage`

The application only starts if the ConfigMap and Secret exists. Otherwise it fails
to start.

## Running

The Docker image can be executed like this:

```
docker run --rm -ti -v $(pwd)/config.yaml:/etc/jira-chat-notifier/config.yaml:ro local/jcn:latest
```

## TODO

* Test with Slack
* Go testing
