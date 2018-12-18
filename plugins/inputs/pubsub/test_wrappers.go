package pubsub

import (
	"time"
	"sync"
	"context"
)

type testSub struct {
	id string
	messages chan *testMsg
}

func (s *testSub) ID() string {
	return s.id
}

func (s *testSub) Receive(ctx context.Context, f func(context.Context, message)) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case m := <- s.messages:
			f(ctx, m)
		}
	}
	return nil
}

type testMsg struct {
	id string
	value string
	attributes map[string]string
	publishTime time.Time

	tracker *testTracker
}

func (tm *testMsg) Ack() {
	tm.tracker.Ack()
}

func (tm *testMsg) Nack() {
	tm.tracker.Nack()
}

func (tm *testMsg) ID() string {
	return tm.id
}

func (tm *testMsg) Data() []byte {
	return []byte(tm.value)
}

func (tm *testMsg) Attributes() map[string]string {
	return tm.attributes
}

func (tm *testMsg) PublishTime() time.Time {
	return tm.publishTime
}

type testTracker struct {
	sync.Mutex
	*sync.Cond

	numAcks int
	numNacks int
}

func (t *testTracker) WaitForAck(num int) {
	t.Lock()
	if t.Cond == nil {
		t.Cond = sync.NewCond(&t.Mutex)
	}
	for t.numAcks < num {
		t.Wait()
	}
	t.Unlock()
}

func (t *testTracker) WaitForNack(num int) {
	t.Lock()
	if t.Cond == nil {
		t.Cond = sync.NewCond(&t.Mutex)
	}
	for t.numNacks < num {
		t.Wait()
	}
	t.Unlock()
}

func (t *testTracker) Ack() {
	t.Lock()
	defer t.Unlock()

	t.numAcks++
}

func (t *testTracker) Nack() {
	t.Lock()
	defer t.Unlock()

	t.numNacks++
}
