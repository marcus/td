package monitor_test

import (
	"log"
	"time"

	"github.com/marcus/td/pkg/monitor"
)

func ExampleModel_SetTheme() {
	model, err := monitor.NewEmbeddedWithOptions(monitor.EmbeddedOptions{
		BaseDir:  ".",
		Interval: 2 * time.Second,
		Version:  "embedded",
		// Omitted slots inherit td's standalone defaults.
		Theme: monitor.Theme{
			Primary:     "#7C3AED",
			TextPrimary: "#F9FAFB",
			Surface:     "#111827",
			Border:      "#374151",
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = model.Close() }()

	// When the host previews or applies another palette, call SetTheme from
	// the host's Bubble Tea Update goroutine. The running model is not rebuilt.
	err = model.SetTheme(monitor.Theme{
		Primary:       "#2563EB",
		TextPrimary:   "#111827",
		TextSecondary: "#374151",
		Surface:       "#FFFFFF",
		Border:        "#D1D5DB",
	})
	if err != nil {
		log.Printf("reject invalid host theme: %v", err)
	}
}
