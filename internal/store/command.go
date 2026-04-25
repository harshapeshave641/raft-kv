package store

type CommandType string

const (
	CommandSet    CommandType = "set"
	CommandDelete CommandType = "delete"
	CommandNoop   CommandType = "noop"
)

type Command struct {
	Type  CommandType
	Key   string
	Value string
}

type CommandResult struct {
	Value string
	Error string
}
