package realtime

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAckWatermarkOnlyAdvancesContiguously(t *testing.T) {
	a := newAckAggregator()
	sid, trip := uuid.New(), uuid.New()
	c := &Client{}
	for seq := int64(10); seq <= 15; seq++ {
		a.Submitted(sid, trip, seq, c)
	}
	// out-of-order completion: 11, 12 done but 10 still in flight => nothing to ack
	a.Done(sid, 11)
	a.Done(sid, 12)
	if got := a.collect(time.Hour); len(got) != 0 {
		t.Fatalf("watermark must not pass the in-flight seq 10: %+v", got)
	}
	a.Done(sid, 10)
	got := a.collect(time.Hour)
	if len(got) != 1 || got[0].payload.UpToSeq != 12 {
		t.Fatalf("want up_to 12, got %+v", got)
	}
	// a permanently rejected point does not block the watermark and is reported once
	a.Reject(sid, trip, 13, "low_accuracy", c)
	a.Done(sid, 14)
	got = a.collect(time.Hour)
	if len(got) != 1 || got[0].payload.UpToSeq != 14 || len(got[0].payload.Rejected) != 1 || got[0].payload.Rejected[0].Seq != 13 {
		t.Fatalf("%+v", got)
	}
	a.Done(sid, 15)
	got = a.collect(time.Hour)
	if len(got) != 1 || got[0].payload.UpToSeq != 15 || len(got[0].payload.Rejected) != 0 {
		t.Fatalf("rejected must be reported only once: %+v", got)
	}
	if len(a.collect(time.Hour)) != 0 {
		t.Fatal("nothing new => nothing sent")
	}
}

func TestAckDuplicateRepeatsLostAck(t *testing.T) {
	a := newAckAggregator()
	sid, trip, c := uuid.New(), uuid.New(), &Client{}
	a.Submitted(sid, trip, 1, c)
	a.Done(sid, 1)
	a.collect(time.Hour)
	a.Duplicate(sid, 1) // client resent => it never saw the ack
	got := a.collect(time.Hour)
	if len(got) != 1 || got[0].payload.UpToSeq != 1 {
		t.Fatalf("ack must be repeated: %+v", got)
	}
	a.Duplicate(sid, 2) // not covered yet => no ack
	if len(a.collect(time.Hour)) != 0 {
		t.Fatal("a seq above the watermark must not trigger an ack")
	}
}

func TestAckNoSendWithoutOwnerAndIdleCleanup(t *testing.T) {
	a := newAckAggregator()
	now := time.Now()
	a.now = func() time.Time { return now }
	sid, trip, c := uuid.New(), uuid.New(), &Client{}
	a.Submitted(sid, trip, 1, c)
	a.Detach(c)
	a.Done(sid, 1)
	if len(a.collect(time.Hour)) != 0 {
		t.Fatal("no owner => no send")
	}
	now = now.Add(2 * time.Hour)
	a.collect(time.Hour)
	if a.openSessions() != 0 {
		t.Fatal("idle sessions must be forgotten (bounded memory)")
	}
}

func TestOutboxLatestWinsAndBoundedControl(t *testing.T) {
	o := newOutbox()
	u := uuid.NewString()
	for seq := int64(1); seq <= 1000; seq++ {
		o.putLatest(LivePoint{UserID: u, SessionID: s1, Seq: seq})
	}
	o.putLatest(LivePoint{UserID: u, SessionID: s1, Seq: 5}) // stale: ignored
	ctrl, loc := o.drain()
	if len(ctrl) != 0 || len(loc) != 1 || loc[0].Seq != 1000 {
		t.Fatalf("slow reader holds one position per member: %v %v", len(ctrl), loc)
	}
	over := false
	for i := 0; i < ctrlQueueCap+5; i++ {
		over = o.pushCtrl([]byte("x")) || over
	}
	if !over || o.depth() != ctrlQueueCap {
		t.Fatalf("control queue must be bounded at %d and report overflow (depth %d overflow %v)", ctrlQueueCap, o.depth(), over)
	}
	o.close()
	if o.pushCtrl([]byte("y")) {
		t.Fatal("closed outbox ignores pushes")
	}
}
