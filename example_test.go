package flagstack_test

import (
	"context"
	"log"

	flagstack "github.com/flagstack/sdk-go"
)

func ExampleNewClientAndWait() {
	flags, err := flagstack.NewClientAndWait(context.Background(), flagstack.ClientOptions{
		BaseURL:   "https://flags.example.com",
		ServerKey: "fs_server_...",
	})
	if err != nil {
		log.Fatal(err)
	}
	defer flags.Close()

	enabled := flags.Boolean("new-checkout", false, flagstack.EvaluationContext{
		TargetingKey: "user-123",
		Attributes: map[string]any{
			"plan":    "enterprise",
			"country": "GB",
		},
	})
	_ = enabled
}

func ExampleClient_StartPolling() {
	flags, err := flagstack.NewClientAndWait(context.Background(), flagstack.ClientOptions{
		BaseURL:      "https://flags.example.com",
		ServerKey:    "fs_server_...",
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
