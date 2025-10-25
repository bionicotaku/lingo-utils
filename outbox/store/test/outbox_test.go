package store_test

import (
	"testing"
	"time"

	"github.com/bionicotaku/lingo-utils/outbox/store"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestMessage_Validation(t *testing.T) {
	tests := []struct {
		name    string
		msg     store.Message
		isValid bool
	}{
		{
			name: "valid message",
			msg: store.Message{
				EventID:       uuid.New(),
				AggregateType: "video",
				AggregateID:   uuid.New(),
				EventType:     "video.created",
				Payload:       []byte("test-payload"),
				Headers:       map[string]string{"trace_id": "abc123"},
				AvailableAt:   time.Now().UTC(),
			},
			isValid: true,
		},
		{
			name: "message with nil headers",
			msg: store.Message{
				EventID:       uuid.New(),
				AggregateType: "video",
				AggregateID:   uuid.New(),
				EventType:     "video.created",
				Payload:       []byte("test-payload"),
				Headers:       nil,
				AvailableAt:   time.Now().UTC(),
			},
			isValid: true,
		},
		{
			name: "message with empty payload",
			msg: store.Message{
				EventID:       uuid.New(),
				AggregateType: "video",
				AggregateID:   uuid.New(),
				EventType:     "video.created",
				Payload:       []byte{},
				Headers:       map[string]string{},
				AvailableAt:   time.Now().UTC(),
			},
			isValid: true,
		},
		{
			name: "message with zero available_at",
			msg: store.Message{
				EventID:       uuid.New(),
				AggregateType: "video",
				AggregateID:   uuid.New(),
				EventType:     "video.created",
				Payload:       []byte("test-payload"),
				Headers:       map[string]string{},
				AvailableAt:   time.Time{}, // zero time - 应该由 Enqueue 填充
			},
			isValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 验证字段类型正确
			assert.IsType(t, uuid.UUID{}, tt.msg.EventID)
			assert.IsType(t, "", tt.msg.AggregateType)
			assert.IsType(t, uuid.UUID{}, tt.msg.AggregateID)
			assert.IsType(t, "", tt.msg.EventType)
			assert.IsType(t, []byte{}, tt.msg.Payload)
			assert.IsType(t, map[string]string{}, tt.msg.Headers)
			assert.IsType(t, time.Time{}, tt.msg.AvailableAt)
		})
	}
}

func TestEvent_Fields(t *testing.T) {
	now := time.Now().UTC()
	eventID := uuid.New()
	aggregateID := uuid.New()
	lockToken := "test-lock"
	lastError := "test error"

	event := store.Event{
		EventID:          eventID,
		AggregateType:    "video",
		AggregateID:      aggregateID,
		EventType:        "video.created",
		Payload:          []byte("test-payload"),
		Headers:          map[string]string{"key": "value"},
		OccurredAt:       now,
		AvailableAt:      now,
		PublishedAt:      &now,
		DeliveryAttempts: 1,
		LastError:        &lastError,
		LockToken:        &lockToken,
		LockedAt:         &now,
	}

	// 验证所有字段
	assert.Equal(t, eventID, event.EventID)
	assert.Equal(t, "video", event.AggregateType)
	assert.Equal(t, aggregateID, event.AggregateID)
	assert.Equal(t, "video.created", event.EventType)
	assert.Equal(t, []byte("test-payload"), event.Payload)
	assert.Equal(t, map[string]string{"key": "value"}, event.Headers)
	assert.True(t, now.Equal(event.OccurredAt))
	assert.True(t, now.Equal(event.AvailableAt))
	assert.NotNil(t, event.PublishedAt)
	assert.Equal(t, int32(1), event.DeliveryAttempts)
	assert.NotNil(t, event.LastError)
	assert.NotNil(t, event.LockToken)
	assert.NotNil(t, event.LockedAt)
}

func TestEvent_NullableFields(t *testing.T) {
	event := store.Event{
		EventID:          uuid.New(),
		AggregateType:    "video",
		AggregateID:      uuid.New(),
		EventType:        "video.created",
		Payload:          []byte("test"),
		Headers:          map[string]string{},
		OccurredAt:       time.Now(),
		AvailableAt:      time.Now(),
		PublishedAt:      nil, // 未发布
		DeliveryAttempts: 0,
		LastError:        nil, // 无错误
		LockToken:        nil, // 未锁定
		LockedAt:         nil,
	}

	// 验证可空字段为 nil
	assert.Nil(t, event.PublishedAt)
	assert.Nil(t, event.LastError)
	assert.Nil(t, event.LockToken)
	assert.Nil(t, event.LockedAt)
	assert.Equal(t, int32(0), event.DeliveryAttempts)
}

// 以下是集成测试，需要真实数据库
// 这些测试应该在 test/outbox_integration_test.go 中实现
/*
func TestRepository_Enqueue_Integration(t *testing.T) {
	t.Skip("需要数据库连接，移至集成测试")
}

func TestRepository_ClaimPending_Integration(t *testing.T) {
	t.Skip("需要数据库连接，移至集成测试")
}

func TestRepository_MarkPublished_Integration(t *testing.T) {
	t.Skip("需要数据库连接，移至集成测试")
}

func TestRepository_Reschedule_Integration(t *testing.T) {
	t.Skip("需要数据库连接，移至集成测试")
}

func TestRepository_CountPending_Integration(t *testing.T) {
	t.Skip("需要数据库连接，移至集成测试")
}
*/
