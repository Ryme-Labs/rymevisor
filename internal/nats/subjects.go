package nats


const CommandsNodePrefix = "commands.node."


func SubjectForNode(nodeID, action string) string {
	return CommandsNodePrefix + nodeID + "." + action
}
