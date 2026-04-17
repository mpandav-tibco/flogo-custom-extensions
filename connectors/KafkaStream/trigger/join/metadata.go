// Package join provides a Flogo trigger that consumes messages from two or
// more Kafka topics and fires the associated flow when messages sharing the
// same joinKeyField value have been received from every configured topic within
// joinWindowMs milliseconds — a classic stream-join / stream-enrichment pattern.
package join

import (
	"strings"

	"github.com/project-flogo/core/data/coerce"
	"github.com/project-flogo/core/support/connection"
)

// EventType constants used in HandlerSettings to choose which events a handler
// receives.
const (
	// EventTypeJoined is the default — fires when messages from all topics
	// carrying the same joinKeyField value arrive within joinWindowMs.
	EventTypeJoined = "joined"
	// EventTypeTimeout fires when the join window expires before all topics have
	// contributed, providing dead-letter-queue (DLQ) / partial-result semantics.
	EventTypeTimeout = "timeout"
	// EventTypeAll fires the handler for both joined and timeout events.
	EventTypeAll = "all"
)

// Settings hold trigger-level configuration defined at design time.
type Settings struct {
	// ── Kafka Connection ─────────────────────────────────────────────────────
	// Connection is the TIBCO Kafka shared connection (brokers, auth, TLS).
	Connection connection.Manager `md:"kafkaConnection,required"`

	// Topics is a comma-separated list of Kafka topics to join (minimum 2).
	// Example: "orders,payments" or "sensor-a,sensor-b,sensor-c"
	// Each topic gets its own consumer group: <consumerGroup>-<topicName>.
	Topics string `md:"topics,required"`

	// ConsumerGroup is the base consumer group ID.
	// The trigger creates one consumer group per topic, named
	// "<consumerGroup>-<topicName>", e.g. "my-join-cg-orders" and
	// "my-join-cg-payments".
	ConsumerGroup string `md:"consumerGroup,required"`

	// InitialOffset controls where consumption starts when no committed offset
	// exists for the consumer group. "newest" (default) | "oldest".
	InitialOffset string `md:"initialOffset"`

	// ── Join configuration ────────────────────────────────────────────────────
	// JoinKeyField is the message field whose value is used to correlate messages
	// across topics (e.g. "order_id", "device_id").
	JoinKeyField string `md:"joinKeyField,required"`

	// JoinWindowMs is the maximum time in milliseconds to wait for all topics to
	// contribute a message with the same joinKeyField value.  When the window
	// expires the timeout handler fires (if configured) and the partial state is
	// discarded.
	JoinWindowMs int64 `md:"joinWindowMs,required"`

	// ── Consumer group rebalance ──────────────────────────────────────────────
	// BalanceStrategy sets the Kafka consumer group rebalance strategy.
	// "roundrobin" (default) | "sticky" | "range"
	BalanceStrategy string `md:"balanceStrategy"`

	// ── Offset commit ────────────────────────────────────────────────────────
	// CommitOnSuccess controls when the Kafka offset is marked for the
	// completing (last-arriving) topic's message.
	//
	// true (default): only mark the completing message's offset when all
	// handlers complete without error — at-least-once for the completing topic.
	// false: always mark regardless of handler result (at-most-once).
	//
	// Note: offsets for non-completing topics are always marked immediately after
	// their contribution is recorded, because Sarama sessions cannot be held open
	// across topic boundaries.  This is an inherent limitation of multi-topic joins.
	CommitOnSuccess bool `md:"commitOnSuccess"`

	// HandlerTimeoutMs is the maximum time in milliseconds allowed for all
	// handlers to complete.  0 means no timeout.
	HandlerTimeoutMs int64 `md:"handlerTimeoutMs"`

	// ── Join store ────────────────────────────────────────────────────────────
	// StoreType selects the backing store for in-flight join state.
	// "memory" (default) | "file" | "redis"
	//
	// • "memory" — process-local sync.Map; no dependencies; state is lost on
	//              restart or rebalance.
	// • "file"   — wraps memory with a JSON snapshot written on shutdown and
	//              before rebalance; restores on startup and after rebalance.
	//              Requires PersistPath; shared filesystem needed for
	//              multi-instance deployments.
	// • "redis"  — all state lives in Redis; every instance shares the same
	//              in-flight entries; survives restarts and rebalances without
	//              any extra configuration.  Requires RedisAddr.
	StoreType string `md:"storeType"`

	// PersistPath is the absolute file path used for the JSON snapshot when
	// storeType is "file".  Example: "/var/data/flogo/join-state.json"
	PersistPath string `md:"persistPath"`

	// RedisAddr is the Redis server address (host:port) used when storeType is
	// "redis".  Example: "localhost:6379"
	RedisAddr string `md:"redisAddr"`

	// RedisPassword is the Redis AUTH password.  Leave empty when the Redis
	// server does not require authentication.
	RedisPassword string `md:"redisPassword"`

	// RedisDB is the Redis database number (0–15).  0 is the default database.
	RedisDB int `md:"redisDB"`
}

// TopicList parses and returns the trimmed, non-empty topic names from Topics.
func (s *Settings) TopicList() []string {
	var out []string
	for _, t := range strings.Split(s.Topics, ",") {
		t = strings.TrimSpace(t)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

// HandlerSettings define which event type a particular handler (flow) receives.
type HandlerSettings struct {
	// EventType controls when this handler fires.
	// "joined" (default): fires when all topics contribute within the window.
	// "timeout": fires when the join window expires before all topics contribute.
	// "all": fires for both joined and timeout events.
	EventType string `md:"eventType"`
}

// JoinedMessage carries the merged payloads from all participating topics.
type JoinedMessage struct {
	// Messages is a map of topic name → full decoded JSON payload.
	// Example: {"orders": {"order_id":"O1","amount":99.9}, "payments": {"order_id":"O1","status":"paid"}}
	Messages map[string]interface{} `md:"messages"`
	// JoinKey is the value of joinKeyField that triggered the join.
	JoinKey string `md:"joinKey"`
	// Topics is the ordered list of topic names that contributed.
	Topics []string `md:"topics"`
	// JoinedAt is the wall-clock time when the join completed (Unix-ms).
	JoinedAt int64 `md:"joinedAt"`
}

// TimeoutResult carries partial data for a join that expired before completion.
type TimeoutResult struct {
	// PartialMessages is a map of topic name → payload for topics that
	// contributed before the window expired.
	PartialMessages map[string]interface{} `md:"partialMessages"`
	// JoinKey that timed out.
	JoinKey string `md:"joinKey"`
	// MissingTopics lists the topics that did not contribute before expiry.
	MissingTopics []string `md:"missingTopics"`
	// CreatedAt is when the join window was first opened (Unix-ms).
	CreatedAt int64 `md:"createdAt"`
}

// Output is the data sent to the Flogo flow when this trigger fires.
type Output struct {
	// JoinResult is populated when EventType is "joined"; zero otherwise.
	JoinResult JoinedMessage `md:"joinResult"`
	// TimeoutResult is populated when EventType is "timeout"; zero otherwise.
	TimeoutResult TimeoutResult `md:"timeoutResult"`
	// EventType indicates which event fired: "joined" or "timeout".
	EventType string `md:"eventType"`
}

func (o *Output) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"joinResult": map[string]interface{}{
			"messages": o.JoinResult.Messages,
			"joinKey":  o.JoinResult.JoinKey,
			"topics":   o.JoinResult.Topics,
			"joinedAt": o.JoinResult.JoinedAt,
		},
		"timeoutResult": map[string]interface{}{
			"partialMessages": o.TimeoutResult.PartialMessages,
			"joinKey":         o.TimeoutResult.JoinKey,
			"missingTopics":   o.TimeoutResult.MissingTopics,
			"createdAt":       o.TimeoutResult.CreatedAt,
		},
		"eventType": o.EventType,
	}
}

func (o *Output) FromMap(values map[string]interface{}) error {
	var err error
	o.EventType, err = coerce.ToString(values["eventType"])
	if err != nil {
		return err
	}
	if jr, ok := values["joinResult"]; ok {
		jrMap, err := coerce.ToObject(jr)
		if err != nil {
			return err
		}
		o.JoinResult.JoinKey, _ = coerce.ToString(jrMap["joinKey"])
		o.JoinResult.JoinedAt, _ = coerce.ToInt64(jrMap["joinedAt"])
		if msgs, ok := jrMap["messages"]; ok {
			o.JoinResult.Messages, _ = coerce.ToObject(msgs)
		}
	}
	if tr, ok := values["timeoutResult"]; ok {
		trMap, err := coerce.ToObject(tr)
		if err != nil {
			return err
		}
		o.TimeoutResult.JoinKey, _ = coerce.ToString(trMap["joinKey"])
		o.TimeoutResult.CreatedAt, _ = coerce.ToInt64(trMap["createdAt"])
		if msgs, ok := trMap["partialMessages"]; ok {
			o.TimeoutResult.PartialMessages, _ = coerce.ToObject(msgs)
		}
	}
	return nil
}
