package history

import (
	"database/sql/driver"
	"strings"

	"modernc.org/sqlite"
)

func init() {
	// SQLite's built-in lower() only handles ASCII. Broker names and replies
	// also use accented letters; keep response and Go-side task search aligned.
	sqlite.MustRegisterDeterministicScalarFunction("eraser_contains", 2, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		value, _ := args[0].(string)
		query, _ := args[1].(string)
		if strings.Contains(strings.ToLower(value), strings.ToLower(query)) {
			return int64(1), nil
		}
		return int64(0), nil
	})
}
