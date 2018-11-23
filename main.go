package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/buger/jsonparser"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

var (
	webhooksProcessed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "jira_webhooks_processed_total",
		Help: "The total number of processed JIRA webhook events since application start",
	})
	webhooksProcessedPerProject = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "jira_webhooks_processed_per_project",
		Help: "The total number of processed JIRA webhook events since application start per JIRA project",
	}, []string{"project"})
)

// Fields of interest in an incoming JIRA Webhook
type jiraWebhook struct {
	WebhookEvent  string
	DisplayName   string
	IssueKey      string
	IssueSummary  string
	Project       string
	ProjectAvatar string
	Changelog     string
}

// Rocket.Chat Webhook attachment type
type rocketChatWebhookAttachment struct {
	Title     string `json:"title"`
	TitleLink string `json:"title_link"`
	Text      string `json:"text"`
	ImageURL  string `json:"image_url"`
	Color     string `json:"color"`
}

// Rocket.Chat Webhook type
type rocketChatWebhook struct {
	Text        string                        `json:"text"`
	Attachments []rocketChatWebhookAttachment `json:"attachments"`
}

func sendChatMessage(eventData jiraWebhook, msg string, event string) {

	ticketURLConfigPath := "projects." + eventData.Project + ".ticket_url"

	var ticketURL string
	if viper.IsSet(ticketURLConfigPath) {
		ticketURL = viper.GetString(ticketURLConfigPath)
	} else {
		ticketURL = viper.GetString("general.ticket_url")
	}

	// Compose chat message payload
	chatmsgatt := rocketChatWebhookAttachment{
		Title:     eventData.IssueSummary,
		TitleLink: ticketURL + eventData.IssueKey,
		Text:      msg,
		//ImageURL:  eventData.ProjectAvatar,
	}
	chatmsg := rocketChatWebhook{
		Text:        "Issue " + eventData.IssueKey + " has been " + event,
		Attachments: []rocketChatWebhookAttachment{chatmsgatt},
	}

	bytesRepresentation, _ := json.Marshal(chatmsg)
	webhookEndpoint := viper.GetString("projects." + eventData.Project + ".webhook")

	log.WithFields(log.Fields{
		"jira_project":     eventData.Project,
		"issue_key":        eventData.IssueKey,
		"webhook_endpoint": webhookEndpoint,
	}).Info("Sending webhook to chat")

	res, err := http.Post(webhookEndpoint, "application/json", bytes.NewBuffer(bytesRepresentation))

	if err == nil {
		log.Info("Webhook sent: " + string(res.Status))
	} else {
		log.Error("Webhook sending failed: " + string(res.Status))
	}
}

func jiraIncomingWebhook(rw http.ResponseWriter, req *http.Request) {
	// Read incoming HTTP data
	body, err := ioutil.ReadAll(req.Body)
	if err != nil {
		panic(err)
	}

	// Extract data from incoming webhook JSON payload
	event := jiraWebhook{}
	event.WebhookEvent, _ = jsonparser.GetString(body, "webhookEvent")
	event.DisplayName, _ = jsonparser.GetString(body, "user", "displayName")
	event.IssueKey, _ = jsonparser.GetString(body, "issue", "key")
	event.IssueSummary, _ = jsonparser.GetString(body, "issue", "fields", "summary")
	event.Project, _ = jsonparser.GetString(body, "issue", "fields", "project", "key")
	event.ProjectAvatar, _ = jsonparser.GetString(body, "issue", "fields", "project", "avatarUrls", "24x24")

	if event.WebhookEvent == "" {
		log.Error("webhookEvent field not found")
		rw.WriteHeader(http.StatusInternalServerError)
		rw.Write([]byte("500 - webhookEvent field not found"))
		return
	}

	// Only handle webhooks for known projects
	if !viper.IsSet("projects." + event.Project) {
		log.WithFields(log.Fields{
			"jira_event":   event.WebhookEvent,
			"jira_project": event.Project,
			"issue_key":    event.IssueKey,
		}).Info("JIRA project is unknown in configuration")
		return
	}

	// Handle events
	switch event.WebhookEvent {
	case "jira:issue_created":
		log.WithFields(log.Fields{
			"jira_event":   "issue_created",
			"jira_project": event.Project,
			"issue_key":    event.IssueKey,
		}).Info("JIRA webhook received")
		msg := "By " + event.DisplayName
		sendChatMessage(event, msg, "created")

	case "jira:issue_updated":
		log.WithFields(log.Fields{
			"jira_event":   "issue_updated",
			"jira_project": event.Project,
			"issue_key":    event.IssueKey,
		}).Info("JIRA webhook received")

		// Compose changelog
		changelogFrom, _ := jsonparser.GetString(body, "changelog", "items", "[0]", "fromString")
		changelogTo, _ := jsonparser.GetString(body, "changelog", "items", "[0]", "toString")
		changelogField, _ := jsonparser.GetString(body, "changelog", "items", "[0]", "field")
		event.Changelog = " changed field " + changelogField + ": " + changelogFrom + " -> " + changelogTo

		msg := event.DisplayName + event.Changelog
		sendChatMessage(event, msg, "updated")
	}

	// Increase webhooks processed counter for metrics
	webhooksProcessed.Inc()
	webhooksProcessedPerProject.WithLabelValues(event.Project).Inc()

}

func healthz(rw http.ResponseWriter, req *http.Request) {
	fmt.Fprintf(rw, "ok")
}

func appInfo(rw http.ResponseWriter, req *http.Request) {
	if req.URL.Path != "/" {
		errorHandler(rw, req, http.StatusNotFound)
		return
	}
	fmt.Fprintf(rw, "Welcome to the JIRA webhook to chat bridge\n")
}

func errorHandler(rw http.ResponseWriter, req *http.Request, status int) {
	rw.WriteHeader(status)
}

func main() {
	// Log in JSON
	log.SetFormatter(new(log.JSONFormatter))
	log.Info("Starting JIRA Webhook receiver and chat sender")

	// Read configuration file and set defaults
	viper.SetConfigName("config")
	viper.AddConfigPath(".")
	viper.AddConfigPath("/etc/jira-chat-notifier/")
	err := viper.ReadInConfig()
	if err != nil {
		log.Fatal(fmt.Errorf("config file error: %s", err))
	}
	viper.SetDefault("general.listen", ":8081")

	// Secret must be configured!
	if !viper.IsSet("general.secret") {
		log.Fatal("general.secret not configured")
	}

	if !viper.IsSet("general.ticket_url") {
		log.Warn("general.ticket_url not configured")
	}

	// Handle various URL paths
	http.HandleFunc("/", appInfo)
	http.HandleFunc("/"+viper.GetString("general.secret")+"/jira", jiraIncomingWebhook)
	http.HandleFunc("/healthz", healthz)
	http.Handle("/metrics", promhttp.Handler())

	srv := http.Server{Addr: viper.GetString("general.listen")}
	log.Info("Listening on " + viper.GetString("general.listen"))

	idleConnsClosed := make(chan struct{})
	go func() {
		sigint := make(chan os.Signal, 1)

		// interrupt signal sent from terminal
		signal.Notify(sigint, os.Interrupt)
		// sigterm signal sent from kubernetes
		signal.Notify(sigint, syscall.SIGTERM)

		<-sigint

		// We received an interrupt signal, shut down.
		log.Info("Byebye")
		if err := srv.Shutdown(context.Background()); err != nil {
			// Error from closing listeners, or context timeout
			log.Fatal(fmt.Sprintf("HTTP server shutdown error: %v", err))
		}
		close(idleConnsClosed)
	}()
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal(fmt.Sprintf("Cannot start HTTP server: %v", err))
	}
	<-idleConnsClosed
}
