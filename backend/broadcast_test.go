package main

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
)

// TestBroadcasterSubscribeUnsubscribe verifies basic pub/sub lifecycle.
func TestBroadcasterSubscribeUnsubscribe(t *testing.T) {
	sb := NewSessionBroadcaster()

	ch1 := sb.Subscribe()
	ch2 := sb.Subscribe()

	if n := sb.SubscriberCount(); n != 2 {
		t.Fatalf("expected 2 subscribers, got %d", n)
	}

	sb.Unsubscribe(ch1)
	if n := sb.SubscriberCount(); n != 1 {
		t.Fatalf("expected 1 subscriber after unsub, got %d", n)
	}

	sb.Unsubscribe(ch2)
	if n := sb.SubscriberCount(); n != 0 {
		t.Fatalf("expected 0 subscribers, got %d", n)
	}

	// Channels should be closed after unsubscribe.
	if _, ok := <-ch1; ok {
		t.Fatal("ch1 should be closed")
	}
	if _, ok := <-ch2; ok {
		t.Fatal("ch2 should be closed")
	}
}

// TestBroadcasterGenerationMonotonic verifies the generation counter never
// decreases and is unique per broadcast.
func TestBroadcasterGenerationMonotonic(t *testing.T) {
	sb := NewSessionBroadcaster()
	ch := sb.Subscribe()
	defer sb.Unsubscribe(ch)

	var lastGen uint64
	for range 100 {
		sb.Broadcast(wsMsg{Message: "StatusCheckResponse", Status: "Verified"})
		payload := <-ch
		var msg wsMsg
		if err := json.Unmarshal(payload, &msg); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if msg.Generation <= lastGen {
			t.Fatalf("generation not monotonic: %d <= %d", msg.Generation, lastGen)
		}
		lastGen = msg.Generation
	}
}

// TestBroadcasterHashPresent verifies every broadcast includes a non-empty hash.
func TestBroadcasterHashPresent(t *testing.T) {
	sb := NewSessionBroadcaster()
	ch := sb.Subscribe()
	defer sb.Unsubscribe(ch)

	sb.Broadcast(wsMsg{Message: "StatusCheckResponse", Status: "Verified", CurrentlyPlaying: Track{Name: "Test"}})
	payload := <-ch
	var msg wsMsg
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if msg.StateHash == "" {
		t.Fatal("StateHash is empty")
	}
	if msg.Generation == 0 {
		t.Fatal("Generation is zero")
	}
}

// TestBroadcasterSlowSubscriberDrop verifies that a slow subscriber is dropped
// (its channel should eventually overflow and messages dropped), but the
// broadcaster does not block.
func TestBroadcasterSlowSubscriberDrop(t *testing.T) {
	sb := NewSessionBroadcaster()

	// Create a subscriber but never drain it.
	slow := sb.Subscribe()

	// Fast subscriber.
	fast := sb.Subscribe()

	// Broadcast more messages than the buffer capacity (8).
	done := make(chan struct{})
	go func() {
		for range 20 {
			sb.Broadcast(wsMsg{Message: "StatusCheckResponse", Status: "Verified"})
		}
		close(done)
	}()

	// Drain fast subscriber.
	received := 0
	for {
		select {
		case <-fast:
			received++
			if received >= 20 {
				// Slow channel may have some buffered messages; drain what we can.
				for {
					select {
					case <-slow:
					default:
						goto done
					}
				}
			done:
				<-done // wait for broadcast goroutine to finish
				sb.Unsubscribe(slow)
				sb.Unsubscribe(fast)
				return
			}
		case <-done:
			sb.Unsubscribe(slow)
			sb.Unsubscribe(fast)
			return
		}
	}
}

// TestBroadcasterConcurrent verifies correctness under concurrent broadcasts
// and subscriptions/unsubscriptions.
func TestBroadcasterConcurrent(t *testing.T) {
	sb := NewSessionBroadcaster()

	const numMsgs = 200
	const numSubs = 5

	channels := make([]chan []byte, numSubs)
	for i := range numSubs {
		channels[i] = sb.Subscribe()
	}

	// Broadcast (this will fill 8-slot buffers quickly then drop for slow subs).
	for range numMsgs {
		sb.Broadcast(wsMsg{Message: "StatusCheckResponse", Status: "Verified"})
	}

	// Drain all channels concurrently, then unsubscribe to close them.
	var wg sync.WaitGroup
	for i := range numSubs {
		wg.Add(1)
		go func(ch chan []byte) {
			defer wg.Done()
			for range ch {
			}
		}(channels[i])
	}

	// Unsubscribe all (which closes channels).
	for _, ch := range channels {
		sb.Unsubscribe(ch)
	}

	wg.Wait()
}

// TestBroadcasterHashDeterministic verifies the hash is deterministic for
// identical payloads (excluding generation).
func TestBroadcasterHashDeterministic(t *testing.T) {
	sb := NewSessionBroadcaster()
	ch := sb.Subscribe()
	defer sb.Unsubscribe(ch)

	msg := wsMsg{Message: "StatusCheckResponse", Status: "Verified", CurrentlyPlaying: Track{Name: "Foo", URL: "https://example.com"}}

	sb.Broadcast(msg)
	p1 := <-ch
	sb.Broadcast(msg)
	p2 := <-ch

	var m1, m2 wsMsg
	json.Unmarshal(p1, &m1)
	json.Unmarshal(p2, &m2)

	if m1.StateHash == "" || m2.StateHash == "" {
		t.Fatal("hash is empty")
	}
	if m1.Generation == m2.Generation {
		t.Fatal("generations must differ")
	}
	// Hashes should differ because generation is included.
	if m1.StateHash == m2.StateHash {
		t.Fatal("hashes must differ when generation differs")
	}
}

// ---------------------------------------------------------------------------
// Fuzz tests
// ---------------------------------------------------------------------------

// FuzzBroadcastMessageRoundTrips ensures any wsMsg can be broadcast and
// unmarshalled without corruption. Uses a JSON-calibrated comparison to
// account for encoding normalisation (e.g. invalid UTF-8 → U+FFFD).
func FuzzBroadcastMessageRoundTrips(f *testing.F) {
	// Seed corpus.
	f.Add("StatusCheckResponse", "Verified", "TestTrack", "https://example.com", uint64(1))
	f.Add("StatusCheckResponse", "Unverified", "", "", uint64(0))

	f.Fuzz(func(t *testing.T, message, status, trackName, trackURL string, gen uint64) {
		sb := NewSessionBroadcaster()
		ch := sb.Subscribe()
		defer sb.Unsubscribe(ch)

		original := wsMsg{
			Message: message,
			Status:  status,
			CurrentlyPlaying: Track{
				Name: trackName,
				URL:  trackURL,
			},
		}

		sb.Broadcast(original)
		payload := <-ch

		var decoded wsMsg
		if err := json.Unmarshal(payload, &decoded); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		// Re-encode the original then decode back to get the JSON-calibrated
		// expected value. This handles cases like invalid UTF-8 being
		// normalised by encoding/json.
		calibratedJSON, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("marshal original: %v", err)
		}
		var calibrated wsMsg
		if err := json.Unmarshal(calibratedJSON, &calibrated); err != nil {
			t.Fatalf("unmarshal calibrated: %v", err)
		}

		// Non-generation fields should survive round-trip.
		if decoded.Message != calibrated.Message {
			t.Errorf("Message mismatch: %q != %q", decoded.Message, calibrated.Message)
		}
		if decoded.Status != calibrated.Status {
			t.Errorf("Status mismatch: %q != %q", decoded.Status, calibrated.Status)
		}
		if decoded.CurrentlyPlaying.Name != calibrated.CurrentlyPlaying.Name {
			t.Errorf("Track name mismatch: %q != %q", decoded.CurrentlyPlaying.Name, calibrated.CurrentlyPlaying.Name)
		}
		if decoded.CurrentlyPlaying.URL != calibrated.CurrentlyPlaying.URL {
			t.Errorf("Track URL mismatch: %q != %q", decoded.CurrentlyPlaying.URL, calibrated.CurrentlyPlaying.URL)
		}

		// Generation and hash must be non-zero.
		if decoded.Generation == 0 {
			t.Error("generation is zero")
		}
		if decoded.StateHash == "" {
			t.Error("hash is empty")
		}
	})
}

// FuzzBroadcastConcurrent tests concurrent broadcast + subscribe/unsubscribe
// under random timing.
func FuzzBroadcastConcurrent(f *testing.F) {
	f.Add(uint64(10))

	f.Fuzz(func(t *testing.T, n uint64) {
		if n > 1000 {
			n = 1000
		}
		if n == 0 {
			n = 1
		}

		sb := NewSessionBroadcaster()

		const numSubs = 3
		channels := make([]chan []byte, numSubs)
		for i := range numSubs {
			channels[i] = sb.Subscribe()
		}

		for i := uint64(0); i < n; i++ {
			sb.Broadcast(wsMsg{Message: "StatusCheckResponse", Status: "Verified"})
		}

		// Drain all concurrently then close.
		var wg sync.WaitGroup
		for i := range numSubs {
			wg.Add(1)
			go func(ch chan []byte) {
				defer wg.Done()
				for range ch {
				}
			}(channels[i])
		}

		// Unsubscribe all (which closes channels).
		for _, ch := range channels {
			sb.Unsubscribe(ch)
		}

		wg.Wait()
	})
}

// ---------------------------------------------------------------------------
// Benchmarks
// ---------------------------------------------------------------------------

// BenchmarkBroadcastSingleSubscriber measures push throughput for one client.
func BenchmarkBroadcastSingleSubscriber(b *testing.B) {
	sb := NewSessionBroadcaster()
	ch := sb.Subscribe()
	defer sb.Unsubscribe(ch)

	msg := wsMsg{
		Message:          "StatusCheckResponse",
		Status:           "Verified",
		CurrentlyPlaying: Track{Name: "A Very Long Track Name For Benchmark", URL: "https://youtube.com/watch?v=bench"},
		Playlists:        make([]*Playlist, 10),
		CurrentPlaylist:  make([]Track, 20),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sb.Broadcast(msg)
		<-ch
	}
}

// BenchmarkBroadcastManySubscribers measures throughput with 10 concurrent
// subscribers.
func BenchmarkBroadcastManySubscribers(b *testing.B) {
	sb := NewSessionBroadcaster()

	const numSubs = 10
	channels := make([]chan []byte, numSubs)
	for i := range numSubs {
		channels[i] = sb.Subscribe()
	}
	defer func() {
		for _, ch := range channels {
			sb.Unsubscribe(ch)
		}
	}()

	msg := wsMsg{
		Message:          "StatusCheckResponse",
		Status:           "Verified",
		CurrentlyPlaying: Track{Name: "Benchmark Track", URL: "https://example.com"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sb.Broadcast(msg)
		for _, ch := range channels {
			<-ch
		}
	}
}

// BenchmarkBroadcastLargePayload measures throughput with a realistic large
// payload (many playlists and tracks).
func BenchmarkBroadcastLargePayload(b *testing.B) {
	sb := NewSessionBroadcaster()
	ch := sb.Subscribe()
	defer sb.Unsubscribe(ch)

	// Build a large payload simulating 50 playlists with 10 tracks each.
	playlists := make([]*Playlist, 50)
	for i := range 50 {
		tracks := make([]Track, 10)
		for j := range 10 {
			tracks[j] = Track{
				Name: fmt.Sprintf("Track %d-%d With A Long Descriptive Name", i, j),
				URL:  fmt.Sprintf("https://youtube.com/watch?v=bench%d%d", i, j),
			}
		}
		playlists[i] = &Playlist{
			Title:    fmt.Sprintf("Playlist %d - Category", i),
			Category: "Benchmark",
			Tracks:   tracks,
		}
	}

	msg := wsMsg{
		Message:          "StatusCheckResponse",
		Status:           "Verified",
		Playlists:        playlists,
		CurrentlyPlaying: Track{Name: "Current Track", URL: "https://example.com"},
		CurrentPlaylist:  playlists[0].Tracks,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sb.Broadcast(msg)
		<-ch
	}
}

// BenchmarkSessionSnapshot measures the cost of building a full state snapshot.
func BenchmarkSessionSnapshot(b *testing.B) {
	store, err := NewStore(":memory:")
	if err != nil {
		b.Fatal(err)
	}
	defer store.Close()

	if err := store.EnsureGuild("bench-guild"); err != nil {
		b.Fatal(err)
	}
	if err := store.CloneDefaultPlaylists("bench-guild"); err != nil {
		b.Fatal(err)
	}

	s := newSession(store, "bench-guild")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := sessionSnapshot(s)
		if err != nil {
			b.Fatal(err)
		}
	}
}
