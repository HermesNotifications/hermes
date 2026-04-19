package messaging

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/hermes-notifications/hermes/internal/observability"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type Client struct {
	conn *nats.Conn
	js   jetstream.JetStream
}

type StreamConfig struct {
	Name     string
	Subjects []string
}

var Streams = []StreamConfig{
	{Name: "NOTIFICATIONS", Subjects: []string{"notification.send"}},
	{Name: "DELIVERY", Subjects: []string{"delivery.email", "delivery.sms", "delivery.inbox"}},
	{Name: "EVENTS", Subjects: []string{"notification.events"}},
}

func Connect(url string) (*Client, error) {
	nc, err := nats.Connect(url)
	if err != nil {
		return nil, fmt.Errorf("nats connect: %w", err)
	}
	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("jetstream: %w", err)
	}
	return &Client{conn: nc, js: js}, nil
}

func (c *Client) SetupStreams(ctx context.Context) error {
	for _, s := range Streams {
		_, err := c.js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
			Name:      s.Name,
			Subjects:  s.Subjects,
			Retention: jetstream.WorkQueuePolicy,
			Storage:   jetstream.FileStorage,
			MaxAge:    7 * 24 * time.Hour,
		})
		if err != nil {
			return fmt.Errorf("create stream %s: %w", s.Name, err)
		}
	}
	return nil
}

func (c *Client) Publish(ctx context.Context, subject string, data []byte) error {
	msg := &nats.Msg{Subject: subject, Data: data}
	_, span := observability.InjectNATS(ctx, msg)
	defer span.End()

	_, err := c.js.PublishMsg(ctx, msg)
	observability.RecordError(span, err)
	return err
}

// PermanentError is an interface for errors that should not be retried.
// When a handler returns an error implementing this interface with Permanent() == true,
// the message is terminated (will never be redelivered).
type PermanentError interface {
	Permanent() bool
}

const (
	// maxDeliveries is the maximum number of times a message will be delivered
	// before being dropped. After this many failed attempts the message is dead.
	maxDeliveries = 10
)

// retryDelay returns an exponential backoff delay with jitter.
// Base delay doubles each attempt (1s, 2s, 4s, …) capped at 240s,
// then jitter picks a uniform random duration in [base/2, base].
func retryDelay(attempt uint64) time.Duration {
	base := time.Second << (attempt - 1)
	if base > 240*time.Second {
		base = 240 * time.Second
	}
	half := base / 2
	return half + time.Duration(rand.Int64N(int64(half)+1))
}

// DeliveryInfo is passed to handlers so they can react to delivery context.
type DeliveryInfo struct {
	// Attempt is the 1-based delivery attempt number.
	Attempt uint64
	// LastAttempt is true when this is the final delivery before the message is dropped.
	LastAttempt bool
}

func (c *Client) Subscribe(subject, consumer string, maxAckPending, concurrency int, handler func(ctx context.Context, data []byte, info DeliveryInfo) error) error {
	streamName := ""
	for _, s := range Streams {
		for _, subj := range s.Subjects {
			if subj == subject {
				streamName = s.Name
				break
			}
		}
	}
	if streamName == "" {
		return fmt.Errorf("no stream found for subject %s", subject)
	}

	cons, err := c.js.CreateOrUpdateConsumer(context.Background(), streamName, jetstream.ConsumerConfig{
		Durable:       consumer,
		FilterSubject: subject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		MaxAckPending: maxAckPending,
		MaxDeliver:    maxDeliveries,
	})
	if err != nil {
		return fmt.Errorf("create consumer: %w", err)
	}

	for i := 0; i < concurrency; i++ {
		_, err = cons.Consume(func(msg jetstream.Msg) {
			ctx, span := observability.ExtractNATS(context.Background(), msg.Headers(), msg.Subject())
			defer span.End()

			meta, _ := msg.Metadata()
			attempt := uint64(1)
			if meta != nil {
				attempt = meta.NumDelivered
			}
			info := DeliveryInfo{
				Attempt:     attempt,
				LastAttempt: attempt >= maxDeliveries,
			}

			if err := handler(ctx, msg.Data(), info); err != nil {
				var pe PermanentError
				if errors.As(err, &pe) && pe.Permanent() {
					_ = msg.Term()
				} else {
					_ = msg.NakWithDelay(retryDelay(attempt))
				}
				return
			}
			_ = msg.Ack()
		})
		if err != nil {
			return fmt.Errorf("start consumer %d: %w", i, err)
		}
	}
	return nil
}

func (c *Client) Close() {
	c.conn.Close()
}
