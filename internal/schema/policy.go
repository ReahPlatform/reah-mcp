package schema

// RootScopes is the closed set of upstream query entry points and the argument
// that must always be bound to the configured entity by the server.
var RootScopes = map[string]string{
	"entity":           "id",
	"statementExports": "entityId",
	"cardTransactions": "entityId",
	"invoice":          "entityId",
	"globalSearch":     "entityId",
}
