package apple

import (
	"context"
	"testing"
	"time"

	"git-ui/internal/ai"
)

func TestSendDeliversWhenAReaderIsReady(t *testing.T) {
	ch := make(chan ai.Chunk, 1)
	if !send(context.Background(), ch, ai.Chunk{Delta: "x"}) {
		t.Fatal("send returned false though the channel had room")
	}
	if got := <-ch; got.Delta != "x" {
		t.Fatalf("got = %#v", got)
	}
}

func TestSendReturnsFalseWithoutBlockingWhenContextIsDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ch := make(chan ai.Chunk) // unbuffered; nobody ever reads from it

	done := make(chan bool, 1)
	go func() { done <- send(ctx, ch, ai.Chunk{Delta: "x"}) }()

	select {
	case ok := <-done:
		if ok {
			t.Fatal("send reported success though nobody was reading and the context was already done")
		}
	case <-time.After(time.Second):
		t.Fatal("send blocked forever on an unread channel after the context was done")
	}
}
