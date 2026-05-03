package store

type CommandType string

const (
	CommandSet             CommandType = "set"
	CommandDelete          CommandType = "delete"
	CommandDeleteNamespace CommandType = "delete_namespace"
	CommandNoop            CommandType = "noop"
)

type Command struct {
	Type      CommandType
	Namespace string
	Key       string
	Value     string
}

type CommandResult struct {
	Value string
	Error string
}
