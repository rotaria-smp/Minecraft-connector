package entities

type Topic string

const (
	TopicChat      Topic = "chat"
	TopicStatus    Topic = "status"
	TopicJoin      Topic = "join"
	TopicLeave     Topic = "leave"
	TopicLifecycle Topic = "lifecycle"
	TopicCommand   Topic = "command"
)

// string getter
func (t Topic) String() string {
	return string(t)
}
