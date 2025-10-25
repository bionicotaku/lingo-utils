package store_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/bionicotaku/lingo-utils/outbox/store"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 集成测试需要真实数据库
// 运行: DATABASE_URL=postgresql://... go test -tags=integration ./store/test

// testSession 是一个简单的 Session wrapper 用于测试
type testSession struct {
	ctx context.Context
	tx  pgx.Tx
}

func (s *testSession) Tx() pgx.Tx {
	return s.tx
}

func (s *testSession) Context() context.Context {
	return s.ctx
}

func newTestSession(ctx context.Context, tx pgx.Tx) *testSession {
	return &testSession{ctx: ctx, tx: tx}
}

func setupTestDB(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()

	// 从环境变量获取数据库连接
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	require.NoError(t, err, "failed to connect to database")

	// 验证连接
	require.NoError(t, pool.Ping(ctx), "failed to ping database")

	cleanup := func() {
		pool.Close()
	}

	return pool, cleanup
}

func TestRepository_Outbox_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	logger := log.NewStdLogger(os.Stdout)
	repo := store.NewRepository(pool, logger)

	t.Run("Enqueue and CountPending", func(t *testing.T) {
		// 清理测试数据
		_, err := pool.Exec(ctx, "TRUNCATE TABLE outbox_events CASCADE")
		require.NoError(t, err)

		// 1. Enqueue 事件
		msg := store.Message{
			EventID:       uuid.New(),
			AggregateType: "video",
			AggregateID:   uuid.New(),
			EventType:     "video.created",
			Payload:       []byte("test-payload"),
			Headers:       map[string]string{"trace_id": "test-123"},
			AvailableAt:   time.Now().UTC(),
		}

		err = repo.Enqueue(ctx, nil, msg)
		require.NoError(t, err, "Enqueue should succeed")

		// 2. CountPending 应该返回 1
		count, err := repo.CountPending(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(1), count, "should have 1 pending event")
	})

	t.Run("ClaimPending", func(t *testing.T) {
		// 清理测试数据
		_, err := pool.Exec(ctx, "TRUNCATE TABLE outbox_events CASCADE")
		require.NoError(t, err)

		// 插入测试事件（available_at 设置在过去，确保可以被 claim）
		now := time.Now().UTC()
		msg := store.Message{
			EventID:       uuid.New(),
			AggregateType: "video",
			AggregateID:   uuid.New(),
			EventType:     "video.created",
			Payload:       []byte("test-payload"),
			Headers:       map[string]string{"key": "value"},
			AvailableAt:   now.Add(-time.Minute), // 1分钟前，确保可被认领
		}
		err = repo.Enqueue(ctx, nil, msg)
		require.NoError(t, err)

		// Claim 事件
		// availableBefore: 现在+1小时（认领所有 available_at < now+1h 的事件）
		// staleBefore: 现在-1小时（认领所有 locked_at < now-1h 的陈旧锁）
		events, err := repo.ClaimPending(ctx, now.Add(time.Hour), now.Add(-time.Hour), 10, "test-lock-token")
		require.NoError(t, err)
		require.Len(t, events, 1, "should claim 1 event")

		claimed := events[0]
		assert.Equal(t, msg.EventID, claimed.EventID)
		assert.Equal(t, msg.AggregateType, claimed.AggregateType)
		assert.Equal(t, msg.AggregateID, claimed.AggregateID)
		assert.Equal(t, msg.EventType, claimed.EventType)
		assert.Equal(t, msg.Payload, claimed.Payload)
		assert.Equal(t, msg.Headers, claimed.Headers)
	})

	t.Run("MarkPublished", func(t *testing.T) {
		// 清理测试数据
		_, err := pool.Exec(ctx, "TRUNCATE TABLE outbox_events CASCADE")
		require.NoError(t, err)

		// 插入并 claim 事件
		now := time.Now().UTC()
		msg := store.Message{
			EventID:       uuid.New(),
			AggregateType: "video",
			AggregateID:   uuid.New(),
			EventType:     "video.created",
			Payload:       []byte("test"),
			Headers:       map[string]string{},
			AvailableAt:   now.Add(-time.Minute),
		}
		err = repo.Enqueue(ctx, nil, msg)
		require.NoError(t, err)

		lockToken := "test-lock"
		events, err := repo.ClaimPending(ctx, now.Add(time.Hour), now.Add(-time.Hour), 10, lockToken)
		require.NoError(t, err)
		require.Len(t, events, 1)

		// Mark as published
		publishedAt := time.Now().UTC()
		err = repo.MarkPublished(ctx, nil, msg.EventID, lockToken, publishedAt)
		require.NoError(t, err)

		// Verify published_at is set
		count, err := repo.CountPending(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(0), count, "should have 0 pending events after publish")
	})

	t.Run("Reschedule", func(t *testing.T) {
		// 清理测试数据
		_, err := pool.Exec(ctx, "TRUNCATE TABLE outbox_events CASCADE")
		require.NoError(t, err)

		// 插入并 claim 事件
		now := time.Now().UTC()
		msg := store.Message{
			EventID:       uuid.New(),
			AggregateType: "video",
			AggregateID:   uuid.New(),
			EventType:     "video.created",
			Payload:       []byte("test"),
			Headers:       map[string]string{},
			AvailableAt:   now.Add(-time.Minute),
		}
		err = repo.Enqueue(ctx, nil, msg)
		require.NoError(t, err)

		lockToken := "test-lock"
		events, err := repo.ClaimPending(ctx, now.Add(time.Hour), now.Add(-time.Hour), 10, lockToken)
		require.NoError(t, err)
		require.Len(t, events, 1)

		// Reschedule with error
		nextAvailable := time.Now().Add(5 * time.Minute).UTC()
		err = repo.Reschedule(ctx, nil, msg.EventID, lockToken, nextAvailable, "test error message")
		require.NoError(t, err)

		// Event should still be pending
		count, err := repo.CountPending(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(1), count, "should still have 1 pending event")
	})

	t.Run("Concurrent Enqueue", func(t *testing.T) {
		// 清理测试数据
		_, err := pool.Exec(ctx, "TRUNCATE TABLE outbox_events CASCADE")
		require.NoError(t, err)

		// 并发插入多个事件
		const numGoroutines = 10
		errCh := make(chan error, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func(idx int) {
				msg := store.Message{
					EventID:       uuid.New(),
					AggregateType: "video",
					AggregateID:   uuid.New(),
					EventType:     "video.created",
					Payload:       []byte("concurrent-test"),
					Headers:       map[string]string{"index": string(rune(idx))},
					AvailableAt:   time.Now().UTC(),
				}
				errCh <- repo.Enqueue(ctx, nil, msg)
			}(i)
		}

		// 等待所有 goroutine 完成
		for i := 0; i < numGoroutines; i++ {
			err := <-errCh
			assert.NoError(t, err, "concurrent enqueue should succeed")
		}

		// 验证所有事件都已插入
		count, err := repo.CountPending(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(numGoroutines), count, "should have all events")
	})

	t.Run("Lock Competition - Multiple Workers", func(t *testing.T) {
		// 清理测试数据
		_, err := pool.Exec(ctx, "TRUNCATE TABLE outbox_events CASCADE")
		require.NoError(t, err)

		// 插入 5 个待处理事件
		now := time.Now().UTC()
		for i := 0; i < 5; i++ {
			msg := store.Message{
				EventID:       uuid.New(),
				AggregateType: "video",
				AggregateID:   uuid.New(),
				EventType:     "video.created",
				Payload:       []byte("lock-test"),
				Headers:       map[string]string{},
				AvailableAt:   now.Add(-time.Minute),
			}
			err = repo.Enqueue(ctx, nil, msg)
			require.NoError(t, err)
		}

		// 模拟 3 个 worker 并发认领
		const numWorkers = 3
		type claimResult struct {
			workerID int
			events   []store.Event
			err      error
		}
		resultCh := make(chan claimResult, numWorkers)

		for i := 0; i < numWorkers; i++ {
			go func(workerID int) {
				lockToken := uuid.New().String()
				events, err := repo.ClaimPending(ctx, now.Add(time.Hour), now.Add(-time.Hour), 10, lockToken)
				resultCh <- claimResult{workerID: workerID, events: events, err: err}
			}(i)
		}

		// 收集结果
		totalClaimed := 0
		for i := 0; i < numWorkers; i++ {
			result := <-resultCh
			require.NoError(t, result.err, "worker %d claim should not error", result.workerID)
			totalClaimed += len(result.events)
		}

		// 每个事件只能被一个 worker 认领
		assert.Equal(t, 5, totalClaimed, "each event should be claimed exactly once")
	})

	t.Run("Stale Lock Reclaim", func(t *testing.T) {
		// 清理测试数据
		_, err := pool.Exec(ctx, "TRUNCATE TABLE outbox_events CASCADE")
		require.NoError(t, err)

		// 插入事件
		now := time.Now().UTC()
		msg := store.Message{
			EventID:       uuid.New(),
			AggregateType: "video",
			AggregateID:   uuid.New(),
			EventType:     "video.created",
			Payload:       []byte("stale-lock-test"),
			Headers:       map[string]string{},
			AvailableAt:   now.Add(-time.Minute),
		}
		err = repo.Enqueue(ctx, nil, msg)
		require.NoError(t, err)

		// Worker 1 认领事件
		lockToken1 := "worker-1-lock"
		events, err := repo.ClaimPending(ctx, now.Add(time.Hour), now.Add(-time.Hour), 10, lockToken1)
		require.NoError(t, err)
		require.Len(t, events, 1)

		// 模拟锁已过期（locked_at 在 2 小时前）
		_, err = pool.Exec(ctx,
			"UPDATE outbox_events SET locked_at = $1 WHERE event_id = $2",
			now.Add(-2*time.Hour), msg.EventID,
		)
		require.NoError(t, err)

		// Worker 2 尝试认领陈旧锁的事件（staleBefore 设置为 1 小时前）
		lockToken2 := "worker-2-lock"
		staleEvents, err := repo.ClaimPending(ctx, now.Add(time.Hour), now.Add(-time.Hour), 10, lockToken2)
		require.NoError(t, err)
		assert.Len(t, staleEvents, 1, "should reclaim stale locked event")
		if len(staleEvents) > 0 {
			assert.Equal(t, msg.EventID, staleEvents[0].EventID)
		}
	})

	t.Run("Reschedule and Retry Flow", func(t *testing.T) {
		// 清理测试数据
		_, err := pool.Exec(ctx, "TRUNCATE TABLE outbox_events CASCADE")
		require.NoError(t, err)

		// 插入事件
		now := time.Now().UTC()
		msg := store.Message{
			EventID:       uuid.New(),
			AggregateType: "video",
			AggregateID:   uuid.New(),
			EventType:     "video.created",
			Payload:       []byte("retry-test"),
			Headers:       map[string]string{},
			AvailableAt:   now.Add(-time.Minute),
		}
		err = repo.Enqueue(ctx, nil, msg)
		require.NoError(t, err)

		// 第一次认领
		lockToken1 := "attempt-1"
		events, err := repo.ClaimPending(ctx, now.Add(time.Hour), now.Add(-time.Hour), 10, lockToken1)
		require.NoError(t, err)
		require.Len(t, events, 1)
		assert.Equal(t, int32(0), events[0].DeliveryAttempts, "initial attempts should be 0")

		// Reschedule（模拟发送失败）
		nextAvailable := now.Add(5 * time.Second)
		err = repo.Reschedule(ctx, nil, msg.EventID, lockToken1, nextAvailable, "first attempt failed")
		require.NoError(t, err)

		// 等待 available_at 到达
		time.Sleep(6 * time.Second)

		// 第二次认领
		lockToken2 := "attempt-2"
		retryEvents, err := repo.ClaimPending(ctx, time.Now().Add(time.Hour), time.Now().Add(-time.Hour), 10, lockToken2)
		require.NoError(t, err)
		assert.Len(t, retryEvents, 1, "should be able to claim after reschedule")
		if len(retryEvents) > 0 {
			assert.Equal(t, int32(1), retryEvents[0].DeliveryAttempts, "attempts should increment")
			assert.NotNil(t, retryEvents[0].LastError)
			assert.Equal(t, "first attempt failed", *retryEvents[0].LastError)
		}
	})

	t.Run("MarkPublished with Wrong LockToken", func(t *testing.T) {
		// 清理测试数据
		_, err := pool.Exec(ctx, "TRUNCATE TABLE outbox_events CASCADE")
		require.NoError(t, err)

		// 插入并认领事件
		now := time.Now().UTC()
		msg := store.Message{
			EventID:       uuid.New(),
			AggregateType: "video",
			AggregateID:   uuid.New(),
			EventType:     "video.created",
			Payload:       []byte("lock-token-test"),
			Headers:       map[string]string{},
			AvailableAt:   now.Add(-time.Minute),
		}
		err = repo.Enqueue(ctx, nil, msg)
		require.NoError(t, err)

		correctLockToken := "correct-lock"
		events, err := repo.ClaimPending(ctx, now.Add(time.Hour), now.Add(-time.Hour), 10, correctLockToken)
		require.NoError(t, err)
		require.Len(t, events, 1)

		// 尝试用错误的 lockToken 标记为已发布（应该不影响数据）
		wrongLockToken := "wrong-lock"
		err = repo.MarkPublished(ctx, nil, msg.EventID, wrongLockToken, time.Now().UTC())
		require.NoError(t, err) // SQL 不会报错，只是 WHERE 条件不匹配

		// 验证事件仍然是 pending
		count, err := repo.CountPending(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(1), count, "event should still be pending with wrong lock token")

		// 用正确的 lockToken 标记
		err = repo.MarkPublished(ctx, nil, msg.EventID, correctLockToken, time.Now().UTC())
		require.NoError(t, err)

		count, err = repo.CountPending(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(0), count, "event should be published with correct lock token")
	})

	t.Run("Transaction Rollback - Enqueue", func(t *testing.T) {
		// 清理测试数据
		_, err := pool.Exec(ctx, "TRUNCATE TABLE outbox_events CASCADE")
		require.NoError(t, err)

		// 开始事务
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)

		// 在事务中 enqueue
		sess := newTestSession(ctx, tx)
		msg := store.Message{
			EventID:       uuid.New(),
			AggregateType: "video",
			AggregateID:   uuid.New(),
			EventType:     "video.created",
			Payload:       []byte("tx-rollback-test"),
			Headers:       map[string]string{},
			AvailableAt:   time.Now().UTC(),
		}
		err = repo.Enqueue(ctx, sess, msg)
		require.NoError(t, err)

		// 回滚事务
		err = tx.Rollback(ctx)
		require.NoError(t, err)

		// 验证事件未插入
		count, err := repo.CountPending(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(0), count, "event should not exist after rollback")
	})

	t.Run("Transaction Commit - Enqueue", func(t *testing.T) {
		// 清理测试数据
		_, err := pool.Exec(ctx, "TRUNCATE TABLE outbox_events CASCADE")
		require.NoError(t, err)

		// 开始事务
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)

		// 在事务中 enqueue
		sess := newTestSession(ctx, tx)
		msg := store.Message{
			EventID:       uuid.New(),
			AggregateType: "video",
			AggregateID:   uuid.New(),
			EventType:     "video.created",
			Payload:       []byte("tx-commit-test"),
			Headers:       map[string]string{},
			AvailableAt:   time.Now().UTC(),
		}
		err = repo.Enqueue(ctx, sess, msg)
		require.NoError(t, err)

		// 提交事务
		err = tx.Commit(ctx)
		require.NoError(t, err)

		// 验证事件已插入
		count, err := repo.CountPending(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(1), count, "event should exist after commit")
	})
}

func TestRepository_Inbox_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	logger := log.NewStdLogger(os.Stdout)
	repo := store.NewRepository(pool, logger)

	t.Run("RecordInboxEvent and GetInboxEvent", func(t *testing.T) {
		// 清理测试数据
		_, err := pool.Exec(ctx, "TRUNCATE TABLE inbox_events CASCADE")
		require.NoError(t, err)

		// 记录 inbox 事件
		eventID := uuid.New()
		aggregateType := "video"
		aggregateID := uuid.New().String()

		msg := store.InboxMessage{
			EventID:       eventID,
			SourceService: "catalog",
			EventType:     "video.created",
			AggregateType: &aggregateType,
			AggregateID:   &aggregateID,
			Payload:       []byte("test-payload"),
		}

		err = repo.RecordInboxEvent(ctx, nil, msg)
		require.NoError(t, err, "RecordInboxEvent should succeed")

		// 获取事件
		event, err := repo.GetInboxEvent(ctx, nil, eventID)
		require.NoError(t, err)
		require.NotNil(t, event)

		assert.Equal(t, eventID, event.EventID)
		assert.Equal(t, "catalog", event.SourceService)
		assert.Equal(t, "video.created", event.EventType)
		assert.NotNil(t, event.AggregateType)
		assert.Equal(t, aggregateType, *event.AggregateType)
		assert.NotNil(t, event.AggregateID)
		assert.Equal(t, aggregateID, *event.AggregateID)
		assert.Equal(t, []byte("test-payload"), event.Payload)
		assert.Nil(t, event.ProcessedAt, "should not be processed yet")
		assert.Nil(t, event.LastError, "should not have error")
	})

	t.Run("MarkInboxProcessed", func(t *testing.T) {
		// 清理测试数据
		_, err := pool.Exec(ctx, "TRUNCATE TABLE inbox_events CASCADE")
		require.NoError(t, err)

		// 记录事件
		eventID := uuid.New()
		msg := store.InboxMessage{
			EventID:       eventID,
			SourceService: "catalog",
			EventType:     "video.created",
			Payload:       []byte("test"),
		}
		err = repo.RecordInboxEvent(ctx, nil, msg)
		require.NoError(t, err)

		// 标记为已处理
		processedAt := time.Now().UTC()
		err = repo.MarkInboxProcessed(ctx, nil, eventID, processedAt)
		require.NoError(t, err)

		// 验证
		event, err := repo.GetInboxEvent(ctx, nil, eventID)
		require.NoError(t, err)
		assert.NotNil(t, event.ProcessedAt, "should be marked as processed")
	})

	t.Run("RecordInboxError", func(t *testing.T) {
		// 清理测试数据
		_, err := pool.Exec(ctx, "TRUNCATE TABLE inbox_events CASCADE")
		require.NoError(t, err)

		// 记录事件
		eventID := uuid.New()
		msg := store.InboxMessage{
			EventID:       eventID,
			SourceService: "catalog",
			EventType:     "video.created",
			Payload:       []byte("test"),
		}
		err = repo.RecordInboxEvent(ctx, nil, msg)
		require.NoError(t, err)

		// 记录错误
		errMsg := "processing failed: timeout"
		err = repo.RecordInboxError(ctx, nil, eventID, errMsg)
		require.NoError(t, err)

		// 验证
		event, err := repo.GetInboxEvent(ctx, nil, eventID)
		require.NoError(t, err)
		require.NotNil(t, event.LastError)
		assert.Equal(t, errMsg, *event.LastError)
	})

	t.Run("Idempotent RecordInboxEvent", func(t *testing.T) {
		// 清理测试数据
		_, err := pool.Exec(ctx, "TRUNCATE TABLE inbox_events CASCADE")
		require.NoError(t, err)

		// 记录同一个事件两次（幂等性）
		eventID := uuid.New()
		msg := store.InboxMessage{
			EventID:       eventID,
			SourceService: "catalog",
			EventType:     "video.created",
			Payload:       []byte("test"),
		}

		// 第一次记录
		err = repo.RecordInboxEvent(ctx, nil, msg)
		require.NoError(t, err)

		// 第二次记录应该成功（幂等性）
		// SQL 使用 ON CONFLICT (event_id) DO NOTHING，所以不会报错
		err = repo.RecordInboxEvent(ctx, nil, msg)
		assert.NoError(t, err, "duplicate insert should succeed (idempotent)")

		// 验证只有一条记录
		var count int64
		err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM inbox_events WHERE event_id = $1", eventID).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, int64(1), count, "should only have 1 record despite duplicate insert")
	})

	t.Run("Concurrent Record Different Events", func(t *testing.T) {
		// 清理测试数据
		_, err := pool.Exec(ctx, "TRUNCATE TABLE inbox_events CASCADE")
		require.NoError(t, err)

		// 并发记录多个不同事件
		const numGoroutines = 10
		errCh := make(chan error, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func(idx int) {
				msg := store.InboxMessage{
					EventID:       uuid.New(),
					SourceService: "catalog",
					EventType:     "video.created",
					Payload:       []byte("concurrent-inbox-test"),
				}
				errCh <- repo.RecordInboxEvent(ctx, nil, msg)
			}(i)
		}

		// 等待所有 goroutine 完成
		for i := 0; i < numGoroutines; i++ {
			err := <-errCh
			assert.NoError(t, err, "concurrent record should succeed")
		}

		// 验证所有事件都已记录
		var count int64
		err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM inbox_events").Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, int64(numGoroutines), count, "should have all events")
	})

	t.Run("Processing Error and Retry Flow", func(t *testing.T) {
		// 清理测试数据
		_, err := pool.Exec(ctx, "TRUNCATE TABLE inbox_events CASCADE")
		require.NoError(t, err)

		// 记录事件
		eventID := uuid.New()
		msg := store.InboxMessage{
			EventID:       eventID,
			SourceService: "catalog",
			EventType:     "video.created",
			Payload:       []byte("error-retry-test"),
		}
		err = repo.RecordInboxEvent(ctx, nil, msg)
		require.NoError(t, err)

		// 第一次处理失败
		err = repo.RecordInboxError(ctx, nil, eventID, "first processing failed: network timeout")
		require.NoError(t, err)

		// 验证错误已记录
		event, err := repo.GetInboxEvent(ctx, nil, eventID)
		require.NoError(t, err)
		assert.NotNil(t, event.LastError)
		assert.Equal(t, "first processing failed: network timeout", *event.LastError)
		assert.Nil(t, event.ProcessedAt, "should not be marked as processed")

		// 第二次处理也失败（覆盖之前的错误）
		err = repo.RecordInboxError(ctx, nil, eventID, "second processing failed: database error")
		require.NoError(t, err)

		event, err = repo.GetInboxEvent(ctx, nil, eventID)
		require.NoError(t, err)
		assert.Equal(t, "second processing failed: database error", *event.LastError)

		// 第三次处理成功
		processedAt := time.Now().UTC()
		err = repo.MarkInboxProcessed(ctx, nil, eventID, processedAt)
		require.NoError(t, err)

		event, err = repo.GetInboxEvent(ctx, nil, eventID)
		require.NoError(t, err)
		assert.NotNil(t, event.ProcessedAt, "should be marked as processed")
		assert.NotNil(t, event.LastError, "error should still be recorded")
	})

	t.Run("Transaction Rollback - RecordInboxEvent", func(t *testing.T) {
		// 清理测试数据
		_, err := pool.Exec(ctx, "TRUNCATE TABLE inbox_events CASCADE")
		require.NoError(t, err)

		// 开始事务
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)

		// 在事务中记录事件
		sess := newTestSession(ctx, tx)
		eventID := uuid.New()
		msg := store.InboxMessage{
			EventID:       eventID,
			SourceService: "catalog",
			EventType:     "video.created",
			Payload:       []byte("tx-rollback-test"),
		}
		err = repo.RecordInboxEvent(ctx, sess, msg)
		require.NoError(t, err)

		// 回滚事务
		err = tx.Rollback(ctx)
		require.NoError(t, err)

		// 验证事件未记录
		event, err := repo.GetInboxEvent(ctx, nil, eventID)
		assert.NoError(t, err) // GetInboxEvent 不报错，返回 nil
		assert.Nil(t, event, "event should not exist after rollback")
	})

	t.Run("Transaction Commit - RecordInboxEvent", func(t *testing.T) {
		// 清理测试数据
		_, err := pool.Exec(ctx, "TRUNCATE TABLE inbox_events CASCADE")
		require.NoError(t, err)

		// 开始事务
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)

		// 在事务中记录事件
		sess := newTestSession(ctx, tx)
		eventID := uuid.New()
		msg := store.InboxMessage{
			EventID:       eventID,
			SourceService: "catalog",
			EventType:     "video.created",
			Payload:       []byte("tx-commit-test"),
		}
		err = repo.RecordInboxEvent(ctx, sess, msg)
		require.NoError(t, err)

		// 提交事务
		err = tx.Commit(ctx)
		require.NoError(t, err)

		// 验证事件已记录
		event, err := repo.GetInboxEvent(ctx, nil, eventID)
		require.NoError(t, err)
		assert.NotNil(t, event, "event should exist after commit")
		assert.Equal(t, eventID, event.EventID)
	})

	t.Run("MarkInboxProcessed Idempotency", func(t *testing.T) {
		// 清理测试数据
		_, err := pool.Exec(ctx, "TRUNCATE TABLE inbox_events CASCADE")
		require.NoError(t, err)

		// 记录事件
		eventID := uuid.New()
		msg := store.InboxMessage{
			EventID:       eventID,
			SourceService: "catalog",
			EventType:     "video.created",
			Payload:       []byte("idempotent-processed-test"),
		}
		err = repo.RecordInboxEvent(ctx, nil, msg)
		require.NoError(t, err)

		// 第一次标记为已处理
		processedAt1 := time.Now().UTC()
		err = repo.MarkInboxProcessed(ctx, nil, eventID, processedAt1)
		require.NoError(t, err)

		event, err := repo.GetInboxEvent(ctx, nil, eventID)
		require.NoError(t, err)
		firstProcessedAt := event.ProcessedAt
		require.NotNil(t, firstProcessedAt)

		// 第二次标记为已处理（幂等性，时间戳会被更新）
		time.Sleep(100 * time.Millisecond)
		processedAt2 := time.Now().UTC()
		err = repo.MarkInboxProcessed(ctx, nil, eventID, processedAt2)
		require.NoError(t, err)

		event, err = repo.GetInboxEvent(ctx, nil, eventID)
		require.NoError(t, err)
		assert.NotNil(t, event.ProcessedAt)
		// 时间戳应该被更新
		assert.True(t, event.ProcessedAt.After(*firstProcessedAt), "processed_at should be updated")
	})

	t.Run("GetInboxEvent - Not Found", func(t *testing.T) {
		// 清理测试数据
		_, err := pool.Exec(ctx, "TRUNCATE TABLE inbox_events CASCADE")
		require.NoError(t, err)

		// 查询不存在的事件
		nonExistentID := uuid.New()
		event, err := repo.GetInboxEvent(ctx, nil, nonExistentID)
		assert.NoError(t, err, "should not error on not found")
		assert.Nil(t, event, "should return nil for non-existent event")
	})

	t.Run("RecordInboxError on Non-Existent Event", func(t *testing.T) {
		// 清理测试数据
		_, err := pool.Exec(ctx, "TRUNCATE TABLE inbox_events CASCADE")
		require.NoError(t, err)

		// 尝试为不存在的事件记录错误（SQL 不会报错，只是没有影响行数）
		nonExistentID := uuid.New()
		err = repo.RecordInboxError(ctx, nil, nonExistentID, "some error")
		assert.NoError(t, err, "recording error on non-existent event should not error")
	})
}
