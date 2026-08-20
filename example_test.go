package switchonyourcode_test

import (
	"context"
	"log"

	switchonyourcode "github.com/switchonyourcode/sdk-go"
)

func ExampleNewClientAndWait() {
	flags, err := switchonyourcode.NewClientAndWait(context.Background(), switchonyourcode.ClientOptions{
		BaseURL:   "https://flags.example.com",
		ServerKey: "syoc_server_...",
	})
	if err != nil {
		log.Fatal(err)
	}
	defer flags.Close()

	enabled := flags.Boolean("new-checkout", false, switchonyourcode.EvaluationContext{
		TargetingKey: "user-123",
		Attributes: map[string]any{
			"plan":    "enterprise",
			"country": "GB",
		},
	})
	_ = enabled
}

func ExampleClient_StartPolling() {
	flags, err := switchonyourcode.NewClientAndWait(context.Background(), switchonyourcode.ClientOptions{
		BaseURL:      "https://flags.example.com",
		ServerKey:    "syoc_server_...",
		PollInterval: 0, // zero uses the default 30-second interval
	})
	if err != nil {
		log.Fatal(err)
	}
	defer flags.Close()

	if err := flags.StartPolling(context.Background()); err != nil {
		log.Fatal(err)
	}
	defer flags.StopPolling()
}
