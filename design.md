CommandType {
    set, delete, noop     // NO get
}

Command {
    Type  --> CommandType
    Key   --> string
    Value --> string
}

CommandResult {
    Value string
    Error string
}

StateMachine {
    hashmap (in-memory)
    mutex (sync.RWMutex)

    Apply(Command)        // writes only
    Get(key)              // reads only
    Keys()
    Len()
}