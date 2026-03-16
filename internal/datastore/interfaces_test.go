package datastore

import (
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tphakala/birdnet-go/internal/conf"
)

// createDatabase initializes a temporary database for testing purposes.
// It ensures the database connection is opened and handles potential errors.
//
// The cleanup sequence stops background monitoring goroutines (started by Open)
// before closing the database connection. A brief pause allows the goroutines
// to observe the context cancellation and exit cleanly, preventing "database
// is closed" errors from background integrity checks that may still be running.
func createDatabase(t *testing.T, settings *conf.Settings) Interface {
	t.Helper()
	tempDir := t.TempDir()
	settings.Output.SQLite.Enabled = true
	settings.Output.SQLite.Path = tempDir + "/test.db"

	dataStore := New(settings)

	// Attempt to open a database connection.
	require.NoError(t, dataStore.Open(), "Failed to open database")

	// Ensure the database is closed after the test completes.
	// Stop monitoring first to cancel background goroutines (integrity check),
	// then allow a brief pause for goroutine cleanup before closing the DB.
	t.Cleanup(func() {
		// StopMonitoring cancels the monitoring context, signaling background
		// goroutines (integrity check, connection pool monitoring) to exit.
		// The integrity check goroutine launched by Open() runs PRAGMA quick_check
		// immediately and may still be executing when cleanup runs.
		if sqliteStore, ok := dataStore.(*SQLiteStore); ok {
			sqliteStore.StopMonitoring()
		}

		// Allow background goroutines time to observe context cancellation
		// and exit. This prevents "database is closed" races on CI runners
		// where goroutine scheduling may be delayed under load.
		// Note: ideally StopMonitoring would block until goroutines exit
		// (via sync.WaitGroup), but that requires a production code change
		// tracked separately.
		runtime.Gosched()
		time.Sleep(100 * time.Millisecond)

		assert.NoError(t, dataStore.Close(), "Failed to close datastore")
	})

	return dataStore
}
