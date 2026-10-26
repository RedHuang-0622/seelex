package model

// DurableConversationCount returns the number of visible rows that participate
// in durable history offsets. System bootstrap rows do not consume offsets.
func DurableConversationCount(messages []Message) int {
	count := 0
	for _, message := range messages {
		if message.Role != "system" {
			count++
		}
	}
	return count
}

// BoundConversationTail keeps the tail window with system and durable rows
// counted independently.
func BoundConversationTail(messages []Message, window int) []Message {
	if window <= 0 {
		return nil
	}
	keep := make([]bool, len(messages))
	nonSystem, system := 0, 0
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role == "system" {
			if system < window {
				keep[index] = true
				system++
			}
			continue
		}
		if nonSystem < window {
			keep[index] = true
			nonSystem++
		}
	}
	bounded := make([]Message, 0, nonSystem+system)
	for index, message := range messages {
		if keep[index] {
			bounded = append(bounded, message)
		}
	}
	return bounded
}

// BoundConversationHead keeps the head window with system and durable rows
// counted independently.
func BoundConversationHead(messages []Message, window int) []Message {
	if window <= 0 {
		return nil
	}
	bounded := make([]Message, 0, min(len(messages), window*2))
	nonSystem, system := 0, 0
	for _, message := range messages {
		if message.Role == "system" {
			if system < window {
				bounded = append(bounded, message)
				system++
			}
			continue
		}
		if nonSystem < window {
			bounded = append(bounded, message)
			nonSystem++
		}
	}
	return bounded
}
