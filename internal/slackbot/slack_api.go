package slackbot

import (
	"context"

	"github.com/slack-go/slack"
)

// SlackAPI abstracts the subset of slack.Client methods used by the bot.
// This allows tests to substitute a mock implementation without a live Slack connection.
type SlackAPI interface {
	AuthTest() (response *slack.AuthTestResponse, err error)

	// Messaging
	PostMessage(channelID string, options ...slack.MsgOption) (string, string, error)
	PostEphemeral(channelID, userID string, options ...slack.MsgOption) (string, error)
	UpdateMessage(channelID, timestamp string, options ...slack.MsgOption) (string, string, string, error)
	DeleteMessage(channelID, timestamp string) (string, string, error)

	// Modals
	OpenView(triggerID string, view slack.ModalViewRequest) (*slack.ViewResponse, error)

	// Conversations
	CreateConversation(params slack.CreateConversationParams) (*slack.Channel, error)
	GetConversations(params *slack.GetConversationsParameters) ([]slack.Channel, string, error)
	InviteUsersToConversation(channelID string, users ...string) (*slack.Channel, error)
	JoinConversation(channelID string) (*slack.Channel, string, []string, error)
	OpenConversation(params *slack.OpenConversationParameters) (*slack.Channel, bool, bool, error)

	// Users
	GetUserInfo(userID string) (*slack.User, error)
}

// DecisionProvider abstracts the decision client for testability.
type DecisionProvider interface {
	ListPending(ctx context.Context) ([]Decision, error)
	GetDecision(ctx context.Context, issueID string) (*Decision, error)
	Resolve(ctx context.Context, issueID string, chosenIndex int, rationale, resolvedBy string) (*Decision, error)
	ResolveWithText(ctx context.Context, issueID, text, resolvedBy string) (*Decision, error)
	Cancel(ctx context.Context, issueID string) error
}
