package cmd

import (
	"encoding/json"
	"os"
	"runtime"

	"github.com/spf13/cobra"
)

// RuntimeStats contains runtime metrics for soak testing
type RuntimeStats struct {
	AllocMB      float64 `json:"alloc_mb"`
	SysMB        float64 `json:"sys_mb"`
	NumGC        uint32  `json:"num_gc"`
	NumGoroutine int     `json:"num_goroutine"`
	HeapObjects  uint64  `json:"heap_objects"`
	HeapInuseMB  float64 `json:"heap_inuse_mb"`
}

var debugStatsCmd = &cobra.Command{
	Use:     "debug-stats",
	Short:   "Output runtime memory and goroutine statistics (JSON)",
	Long:    `Outputs Go runtime statistics as JSON for soak/endurance testing analysis.`,
	GroupID: "system",
	RunE: func(cmd *cobra.Command, args []string) error {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)

		stats := RuntimeStats{
			AllocMB:      float64(m.Alloc) / 1024 / 1024,
			SysMB:        float64(m.Sys) / 1024 / 1024,
			NumGC:        m.NumGC,
			NumGoroutine: runtime.NumGoroutine(),
			HeapObjects:  m.HeapObjects,
			HeapInuseMB:  float64(m.HeapInuse) / 1024 / 1024,
		}

		enc := json.NewEncoder(os.Stdout)
		return enc.Encode(stats)
	},
}

func init() {
	rootCmd.AddCommand(debugStatsCmd)
}
