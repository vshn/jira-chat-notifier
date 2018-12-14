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
	"strings"
	"syscall"

	"github.com/buger/jsonparser"
	"github.com/fsnotify/fsnotify"
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
	unknownWebhooksReceived = promauto.NewCounter(prometheus.CounterOpts{
		Name: "jira_unknown_webhooks_received_total",
		Help: "The total number of unknown JIRA webhook events received since application start",
	})
	webhooksProcessedPerProject = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "jira_webhooks_processed_per_project",
		Help: "The total number of processed JIRA webhook events since application start per JIRA project",
	}, []string{"project"})

	// Global config
	config appConfig
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

type generalConfig struct {
	TicketURL string `mapstructure:"ticket_url"`
	Listen    string
	Secret    string
}

type outgoingWebhookConfig struct {
	WebhookURL string `mapstructure:"webhook"`
	TicketURL  string `mapstructure:"ticket_url"`
	OnEvents   string `mapstructure:"on_events"`
}

type appConfig struct {
	General  generalConfig                      `mapstructure:"general"`
	Projects map[string][]outgoingWebhookConfig `mapstructure:"projects"`
}

func sendChatMessage(eventData jiraWebhook, msg string, event string) {

	for _, out := range config.Projects[strings.ToLower(eventData.Project)] {

		if out.WebhookURL == "" {
			log.Error(fmt.Errorf("projects.%s.webhook not configured", eventData.Project))
			return
		}

		// Skip outgoing webhook when on_events specified and event not configured
		if (out.OnEvents != "") && !strings.Contains(out.OnEvents, event) {
			log.WithFields(log.Fields{
				"jira_project":     eventData.Project,
				"issue_key":        eventData.IssueKey,
				"webhook_endpoint": out.WebhookURL,
			}).Info("Skipping outgoing webhook - event not in on_events")
			return
		}

		// Use default ticket_url if not overwriten in project config
		if out.TicketURL == "" {
			out.TicketURL = config.General.TicketURL
		}

		// Compose chat message payload
		chatmsgatt := rocketChatWebhookAttachment{
			Title:     eventData.IssueSummary,
			TitleLink: out.TicketURL + eventData.IssueKey,
			Text:      msg,
			//ImageURL:  eventData.ProjectAvatar,
		}
		chatmsg := rocketChatWebhook{
			Text:        "Issue " + eventData.IssueKey + " has been " + event,
			Attachments: []rocketChatWebhookAttachment{chatmsgatt},
		}

		bytesRepresentation, _ := json.Marshal(chatmsg)

		log.WithFields(log.Fields{
			"jira_project":     eventData.Project,
			"issue_key":        eventData.IssueKey,
			"webhook_endpoint": out.WebhookURL,
		}).Info("Sending webhook to chat")

		res, err := http.Post(out.WebhookURL, "application/json", bytes.NewBuffer(bytesRepresentation))

		if err == nil {
			log.Info("Webhook sent: " + string(res.Status))
		} else {
			log.Error("Webhook sending failed: " + string(res.Status))
		}
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

	// Check for webhookEvent field to identify JIRA webhooks
	if event.WebhookEvent == "" {
		log.Error("webhookEvent field not found")
		rw.WriteHeader(http.StatusInternalServerError)
		rw.Write([]byte("500 - webhookEvent field not found"))
		return
	}

	// Handle known events
	var msg string
	var eventMsg string

	switch event.WebhookEvent {
	case "jira:issue_created":
		msg = "By " + event.DisplayName
		eventMsg = "created"

	case "jira:issue_updated":
		// Compose changelog
		changelogFrom, _ := jsonparser.GetString(body, "changelog", "items", "[0]", "fromString")
		changelogTo, _ := jsonparser.GetString(body, "changelog", "items", "[0]", "toString")
		changelogField, _ := jsonparser.GetString(body, "changelog", "items", "[0]", "field")

		// Sometimes there is no changelog - in this case we don't proceed
		if changelogField != "" {
			event.Changelog = " changed field " + changelogField + ": " + changelogFrom + " -> " + changelogTo

			msg = event.DisplayName + event.Changelog
			eventMsg = "updated"
		} else {
			log.WithFields(log.Fields{
				"jira_event":   event.WebhookEvent,
				"jira_project": event.Project,
				"issue_key":    event.IssueKey,
			}).Warn("Empty changelog - skipping")
		}

	default:
		log.WithFields(log.Fields{
			"jira_event": event.WebhookEvent,
		}).Warn("Unknown JIRA event received. Skipping.")

		unknownWebhooksReceived.Inc()
	}

	// If there is a message ready, check if it's a known project and send message
	if len(msg) > 0 {
		// Only handle event for known projects
		if _, ok := config.Projects[strings.ToLower(event.Project)]; ok {
			log.WithFields(log.Fields{
				"jira_event":   event.WebhookEvent,
				"jira_project": event.Project,
				"issue_key":    event.IssueKey,
			}).Info("Known JIRA event received and matching project config found")

			sendChatMessage(event, msg, eventMsg)

			// Count webhooks per project
			webhooksProcessedPerProject.WithLabelValues(event.Project).Inc()
		} else {
			log.WithFields(log.Fields{
				"jira_event":   event.WebhookEvent,
				"jira_project": event.Project,
				"issue_key":    event.IssueKey,
			}).Warn("JIRA project not found in configuration")
		}
	}

	// Increase webhooks processed counter for metrics
	webhooksProcessed.Inc()
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
		log.Fatal(fmt.Errorf("config file reading error: %v", err))
	}

	// Check if secret is set in config and env var - warn about this fact
	if (os.Getenv("URL_SECRET") != "") && (viper.IsSet("general.secret")) {
		log.Warn("secret configured in config file and env var - env var has precedence")
	}

	// Read URL secret from environment variable
	viper.BindEnv("general.secret", "URL_SECRET")

	// unmarshal config into appConfig struct
	err = viper.Unmarshal(&config)
	if err != nil {
		log.Fatal(fmt.Errorf("config file parsing error: %v", err))
	}

	// Secret must be configured - abort if not set
	if config.General.Secret == "" {
		log.Fatal("general.secret not configured")
	}
	if config.General.TicketURL == "" {
		log.Warn("general.ticket_url not configured")
	}
	if config.General.Listen == "" {
		log.Info("general.listen not configured - defaulting to :8081")
		config.General.Listen = ":8081"
	}

	// Support hot config reloading
	viper.WatchConfig()
	viper.OnConfigChange(func(e fsnotify.Event) {
		log.Info("Configuration file changed, reloading configuration")
		viper.Unmarshal(&config)
	})

	// Handle various URL paths
	http.HandleFunc("/", appInfo)
	http.HandleFunc("/"+config.General.Secret+"/jira", jiraIncomingWebhook)
	http.HandleFunc("/healthz", healthz)
	http.Handle("/metrics", promhttp.Handler())

	srv := http.Server{Addr: config.General.Listen}
	log.Info("Listening on " + config.General.Listen)

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
