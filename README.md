# executor

## SDK quick start

Use `pkg/sdk` when embedding this project as a library.

```go
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/supremeagent/executor/pkg/sdk"
)

func main() {
	ctx := context.Background()
	client := sdk.New()
	defer client.Shutdown()

	// 1) Execute task
	resp, err := client.Execute(ctx, sdk.ExecuteRequest{
		Prompt:     "帮我扫描当前目录并总结关键文件",
		Executor:   sdk.ExecutorCodex, // or sdk.ExecutorClaudeCode
		WorkingDir: ".",
	})
	if err != nil {
		log.Fatal(err)
	}
	sessionID := resp.SessionID
	fmt.Println("session:", sessionID)

	// 2) Stream events via channel
	events, cancel := client.Subscribe(sessionID, sdk.SubscribeOptions{
		ReturnAll:    false,
		IncludeDebug: false,
	})
	defer cancel()

	go func() {
		for evt := range events {
			fmt.Printf("[%s] %v\n", evt.Type, evt.Content)
			if evt.Type == "done" {
				return
			}
		}
	}()

	// 3) Pause and resume task
	time.Sleep(2 * time.Second)
	if err := client.PauseTask(sessionID); err != nil {
		log.Println("pause failed:", err)
	}

	time.Sleep(1 * time.Second)
	if err := client.ResumeTask(ctx, sessionID, "继续执行，给我最终结论"); err != nil {
		log.Println("resume failed:", err)
	}

	// Wait for streaming goroutine in real programs.
	time.Sleep(10 * time.Second)
}
```

## HTTP API

HTTP API is now implemented as a private package outside `pkg`:

- `internal/httpapi`
