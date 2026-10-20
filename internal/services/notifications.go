package services

import (
	"context"
	"fmt"
	"log"
	"strings"

	"arch_erp/internal/config"

	"github.com/resend/resend-go/v2"
)

// NotificationService handles both in-app events and outbound email notifications.
type NotificationService struct {
	client *resend.Client
	from   string
	appURL string
}

// NewNotificationService creates a NotificationService. If the API key is empty,
// email sending is skipped with a log warning (graceful degradation).
func NewNotificationService(cfg *config.Config) *NotificationService {
	var client *resend.Client
	if cfg.ResendAPIKey != "" && !strings.HasPrefix(cfg.ResendAPIKey, "re_your") {
		client = resend.NewClient(cfg.ResendAPIKey)
	} else {
		log.Println("warn: RESEND_API_KEY not configured — email notifications disabled")
	}
	return &NotificationService{
		client: client,
		from:   cfg.ResendFrom,
		appURL: cfg.AppBaseURL,
	}
}

// EmailPayload describes an email to be sent.
type EmailPayload struct {
	To      string
	Subject string
	Body    string
}

// Send dispatches a single email. It is a no-op when Resend is unconfigured.
func (n *NotificationService) Send(ctx context.Context, p EmailPayload) error {
	if n.client == nil {
		log.Printf("email (skipped — no API key): to=%s subject=%s", p.To, p.Subject)
		return nil
	}
	params := &resend.SendEmailRequest{
		From:    n.from,
		To:      []string{p.To},
		Subject: p.Subject,
		Html:    p.Body,
	}
	_, err := n.client.Emails.Send(params)
	if err != nil {
		return fmt.Errorf("resend send: %w", err)
	}
	return nil
}

// NotifyTaskAssigned sends an email when a task is assigned to a user.
func (n *NotificationService) NotifyTaskAssigned(ctx context.Context, recipientEmail, recipientName, taskTitle, projectName string) {
	body := fmt.Sprintf(`
<p>Hi %s,</p>
<p>You have been assigned a new task: <strong>%s</strong> in project <strong>%s</strong>.</p>
<p><a href="%s">View the task →</a></p>
`, recipientName, taskTitle, projectName, n.appURL)

	if err := n.Send(ctx, EmailPayload{
		To:      recipientEmail,
		Subject: fmt.Sprintf("[Arch ERP] New task assigned: %s", taskTitle),
		Body:    body,
	}); err != nil {
		log.Println("warn: failed to send task-assigned email:", err)
	}
}

// NotifyNeedsApproval alerts senior architects when a task is moved to Needs Approval.
func (n *NotificationService) NotifyNeedsApproval(ctx context.Context, emails []string, taskTitle, projectName, movedByName string) {
	body := fmt.Sprintf(`
<p>Hi,</p>
<p><strong>%s</strong> has moved task <strong>%s</strong> to <em>Needs Approval</em> in project <strong>%s</strong>.</p>
<p>Please review and approve or reject it.</p>
<p><a href="%s">Open project board →</a></p>
`, movedByName, taskTitle, projectName, n.appURL)

	for _, email := range emails {
		if err := n.Send(ctx, EmailPayload{
			To:      email,
			Subject: fmt.Sprintf("[Arch ERP] Task needs approval: %s", taskTitle),
			Body:    body,
		}); err != nil {
			log.Printf("warn: failed to send needs-approval email to %s: %v", email, err)
		}
	}
}

// NotifyTaskDone alerts the task assignee when their task is marked Done.
func (n *NotificationService) NotifyTaskDone(ctx context.Context, recipientEmail, recipientName, taskTitle, projectName, approvedByName string) {
	body := fmt.Sprintf(`
<p>Hi %s,</p>
<p>Your task <strong>%s</strong> in project <strong>%s</strong> has been marked as <strong>Done</strong> by %s.</p>
<p><a href="%s">View the board →</a></p>
`, recipientName, taskTitle, projectName, approvedByName, n.appURL)

	if err := n.Send(ctx, EmailPayload{
		To:      recipientEmail,
		Subject: fmt.Sprintf("[Arch ERP] Task completed: %s", taskTitle),
		Body:    body,
	}); err != nil {
		log.Println("warn: failed to send task-done email:", err)
	}
}
