package assembly

import applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"

type conversationChatStateUnit = applicationconversation.ChatStateUnit

// conversationTurnState embeds the extracted ChatState transitions and keeps
// chatRequest factory methods local until the Conversation aggregate migrates.
type conversationTurnState struct {
	*applicationconversation.TurnStateApplication
	*applicationconversation.RequestApplication
}

func newConversationTurnState() *conversationTurnState {
	return &conversationTurnState{
		TurnStateApplication: applicationconversation.NewTurnState(),
		RequestApplication: applicationconversation.NewRequestApplication(applicationconversation.RequestDeps{
			ParseModelEnvelope: parseModelEnvelope,
			WrapModelInput:     wrapModelInput,
		}),
	}
}
