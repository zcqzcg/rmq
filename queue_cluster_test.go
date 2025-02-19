package rmq

import (
	"fmt"
	"github.com/go-redis/redis/v8"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var clusterOp = &redis.ClusterOptions{
	Addrs:    strings.Split("192.169.3.120:6701,192.169.3.120:6702,192.169.3.120:6703,192.169.3.120:6704,192.169.3.120:6705,192.169.3.120:6706", ","),
	Password: "Z6dIm5My",
}

func TestClusterConnections(t *testing.T) {

	flushConn, err := OpenConnectionWithRedisClusterClient("q1", redis.NewClusterClient(clusterOp), nil)
	assert.NoError(t, err)
	assert.NoError(t, flushConn.stopHeartbeat())
	assert.Equal(t, ErrorNotFound, flushConn.stopHeartbeat())
	assert.NoError(t, flushConn.flushDb())

	connection, err := OpenConnectionWithRedisClusterClient("q1", redis.NewClusterClient(clusterOp), nil)
	assert.NoError(t, err)
	require.NotNil(t, connection)
	_, err = NewCleaner(connection).Clean()
	require.NoError(t, err)

	connections, err := connection.getConnections()
	assert.NoError(t, err)
	assert.Len(t, connections, 1) // cleaner connection remains

	conn1, err := OpenConnectionWithRedisClusterClient("q1", redis.NewClusterClient(clusterOp), nil)
	assert.NoError(t, err)
	connections, err = connection.getConnections()
	assert.NoError(t, err)
	assert.Len(t, connections, 2)
	assert.Equal(t, ErrorNotFound, connection.hijackConnection("nope").checkHeartbeat())
	assert.NoError(t, conn1.checkHeartbeat())
	conn2, err := OpenConnectionWithRedisClusterClient("q1", redis.NewClusterClient(clusterOp), nil)
	assert.NoError(t, err)
	connections, err = connection.getConnections()
	assert.NoError(t, err)
	assert.Len(t, connections, 3)
	assert.NoError(t, conn1.checkHeartbeat())
	assert.NoError(t, conn2.checkHeartbeat())

	assert.Equal(t, ErrorNotFound, connection.hijackConnection("nope").stopHeartbeat())
	assert.NoError(t, conn1.stopHeartbeat())
	assert.Equal(t, ErrorNotFound, conn1.checkHeartbeat())
	assert.NoError(t, conn2.checkHeartbeat())
	connections, err = connection.getConnections()
	assert.NoError(t, err)
	assert.Len(t, connections, 3)

	assert.NoError(t, conn2.stopHeartbeat())
	assert.Equal(t, ErrorNotFound, conn1.checkHeartbeat())
	assert.Equal(t, ErrorNotFound, conn2.checkHeartbeat())
	connections, err = connection.getConnections()
	assert.NoError(t, err)
	assert.Len(t, connections, 3)

	assert.NoError(t, connection.stopHeartbeat())
}

func TestClusterConnectionQueues(t *testing.T) {
	connection, err := OpenConnectionWithRedisClusterClient("q1", redis.NewClusterClient(clusterOp), nil)
	assert.NoError(t, err)
	require.NotNil(t, connection)

	assert.NoError(t, connection.unlistAllQueues())
	queues, err := connection.GetOpenQueues()
	assert.NoError(t, err)
	assert.Len(t, queues, 0)

	queue1, err := connection.OpenQueue("conn-q-q1")
	assert.NoError(t, err)
	require.NotNil(t, queue1)
	queues, err = connection.GetOpenQueues()
	assert.NoError(t, err)
	assert.Equal(t, []string{"conn-q-q1"}, queues)
	queues, err = connection.getConsumingQueues()
	assert.NoError(t, err)
	assert.Len(t, queues, 0)
	assert.NoError(t, queue1.StartConsuming(1, time.Millisecond))
	queues, err = connection.getConsumingQueues()
	assert.NoError(t, err)
	assert.Equal(t, []string{"conn-q-q1"}, queues)

	queue2, err := connection.OpenQueue("conn-q-q2")
	assert.NoError(t, err)
	require.NotNil(t, queue2)
	queues, err = connection.GetOpenQueues()
	assert.NoError(t, err)
	assert.Len(t, queues, 2)
	queues, err = connection.getConsumingQueues()
	assert.NoError(t, err)
	assert.Len(t, queues, 1)
	assert.NoError(t, queue2.StartConsuming(1, time.Millisecond))
	queues, err = connection.getConsumingQueues()
	assert.NoError(t, err)
	assert.Len(t, queues, 2)

	<-queue2.StopConsuming()
	assert.NoError(t, queue2.closeInStaleConnection())
	queues, err = connection.GetOpenQueues()
	assert.NoError(t, err)
	assert.Len(t, queues, 2)
	queues, err = connection.getConsumingQueues()
	assert.NoError(t, err)
	assert.Equal(t, []string{"conn-q-q1"}, queues)

	<-queue1.StopConsuming()
	assert.NoError(t, queue1.closeInStaleConnection())
	queues, err = connection.GetOpenQueues()
	assert.NoError(t, err)
	assert.Len(t, queues, 2)
	queues, err = connection.getConsumingQueues()
	assert.NoError(t, err)
	assert.Len(t, queues, 0)

	readyCount, rejectedCount, err := queue1.Destroy()
	assert.NoError(t, err)
	assert.Equal(t, int64(0), readyCount)
	assert.Equal(t, int64(0), rejectedCount)
	queues, err = connection.GetOpenQueues()
	assert.NoError(t, err)
	assert.Equal(t, []string{"conn-q-q2"}, queues)
	queues, err = connection.getConsumingQueues()
	assert.NoError(t, err)
	assert.Len(t, queues, 0)

	assert.NoError(t, connection.stopHeartbeat())
}

func TestClusterQueueCommon(t *testing.T) {
	connection, err := OpenConnectionWithRedisClusterClient("q1", redis.NewClusterClient(clusterOp), nil)
	assert.NoError(t, err)
	require.NotNil(t, connection)

	queue, err := connection.OpenQueue("queue-q")
	assert.NoError(t, err)
	require.NotNil(t, queue)
	_, err = queue.PurgeReady()
	assert.NoError(t, err)
	eventuallyReady(t, queue, 0)
	assert.NoError(t, queue.Publish("queue-d1"))
	eventuallyReady(t, queue, 1)
	assert.NoError(t, queue.Publish("queue-d2"))
	eventuallyReady(t, queue, 2)
	count, err := queue.PurgeReady()
	assert.Equal(t, int64(2), count)
	eventuallyReady(t, queue, 0)
	count, err = queue.PurgeReady()
	assert.Equal(t, int64(0), count)

	queues, err := connection.getConsumingQueues()
	assert.NoError(t, err)
	assert.Len(t, queues, 0)
	assert.NoError(t, queue.StartConsuming(10, time.Millisecond))
	assert.Equal(t, ErrorAlreadyConsuming, queue.StartConsuming(10, time.Millisecond))
	cons1name, err := queue.AddConsumer("queue-cons1", NewTestConsumer("queue-A"))
	assert.NoError(t, err)
	time.Sleep(time.Millisecond)
	queues, err = connection.getConsumingQueues()
	assert.NoError(t, err)
	assert.Len(t, queues, 1)
	consumers, err := queue.getConsumers()
	assert.NoError(t, err)
	assert.Equal(t, []string{cons1name}, consumers)
	_, err = queue.AddConsumer("queue-cons2", NewTestConsumer("queue-B"))
	assert.NoError(t, err)
	consumers, err = queue.getConsumers()
	assert.NoError(t, err)
	assert.Len(t, consumers, 2)

	<-queue.StopConsuming()
	assert.NoError(t, connection.stopHeartbeat())
}

func TestClusterConsumerCommon(t *testing.T) {
	connection, err := OpenConnectionWithRedisClusterClient("q1", redis.NewClusterClient(clusterOp), nil)
	assert.NoError(t, err)
	require.NotNil(t, connection)

	queue1, err := connection.OpenQueue("cons-q")
	assert.NoError(t, err)
	require.NotNil(t, queue1)
	_, err = queue1.PurgeReady()
	assert.NoError(t, err)

	consumer := NewTestConsumer("cons-A")
	consumer.AutoAck = false
	assert.NoError(t, queue1.StartConsuming(10, time.Millisecond))
	_, err = queue1.AddConsumer("cons-cons", consumer)
	assert.NoError(t, err)
	assert.Nil(t, consumer.LastDelivery)

	assert.NoError(t, queue1.Publish("cons-d1"))
	eventuallyReady(t, queue1, 0)
	eventuallyUnacked(t, queue1, 1)
	require.NotNil(t, consumer.LastDelivery)
	assert.Equal(t, "cons-d1", consumer.LastDelivery.Payload())

	assert.NoError(t, queue1.Publish("cons-d2"))
	eventuallyReady(t, queue1, 0)
	eventuallyUnacked(t, queue1, 2)
	assert.Equal(t, "cons-d2", consumer.LastDelivery.Payload())

	assert.NoError(t, consumer.LastDeliveries[0].Ack())
	eventuallyReady(t, queue1, 0)
	eventuallyUnacked(t, queue1, 1)

	assert.NoError(t, consumer.LastDeliveries[1].Ack())
	eventuallyReady(t, queue1, 0)
	eventuallyUnacked(t, queue1, 0)

	assert.Equal(t, ErrorNotFound, consumer.LastDeliveries[0].Ack())

	assert.NoError(t, queue1.Publish("cons-d3"))
	eventuallyReady(t, queue1, 0)
	eventuallyUnacked(t, queue1, 1)
	eventuallyRejected(t, queue1, 0)
	assert.Equal(t, "cons-d3", consumer.LastDelivery.Payload())
	assert.NoError(t, consumer.LastDelivery.Reject())
	eventuallyReady(t, queue1, 0)
	eventuallyUnacked(t, queue1, 0)
	eventuallyRejected(t, queue1, 1)

	assert.NoError(t, queue1.Publish("cons-d4"))
	eventuallyReady(t, queue1, 0)
	eventuallyUnacked(t, queue1, 1)
	eventuallyRejected(t, queue1, 1)
	assert.Equal(t, "cons-d4", consumer.LastDelivery.Payload())
	assert.NoError(t, consumer.LastDelivery.Reject())
	eventuallyReady(t, queue1, 0)
	eventuallyUnacked(t, queue1, 0)
	eventuallyRejected(t, queue1, 2)
	count, err := queue1.PurgeRejected()
	assert.NoError(t, err)
	assert.Equal(t, int64(2), count)
	eventuallyRejected(t, queue1, 0)
	count, err = queue1.PurgeRejected()
	assert.NoError(t, err)
	assert.Equal(t, int64(0), count)

	queue2, err := connection.OpenQueue("cons-func-q")
	assert.NoError(t, err)
	assert.NoError(t, queue2.StartConsuming(10, time.Millisecond))

	payloadChan := make(chan string, 1)
	payload := "cons-func-payload"

	_, err = queue2.AddConsumerFunc("cons-func", func(delivery Delivery) {
		err = delivery.Ack()
		assert.NoError(t, err)
		payloadChan <- delivery.Payload()
	})
	assert.NoError(t, err)

	assert.NoError(t, queue2.Publish(payload))
	eventuallyReady(t, queue2, 0)
	eventuallyUnacked(t, queue2, 0)
	assert.Equal(t, payload, <-payloadChan)

	<-queue1.StopConsuming()
	<-queue2.StopConsuming()
	assert.NoError(t, connection.stopHeartbeat())
}

func TestClusterMulti(t *testing.T) {
	connection, err := OpenConnectionWithRedisClusterClient("q1", redis.NewClusterClient(clusterOp), nil)
	assert.NoError(t, err)
	queue, err := connection.OpenQueue("multi-q")
	assert.NoError(t, err)
	_, err = queue.PurgeReady()
	assert.NoError(t, err)

	for i := 0; i < 20; i++ {
		err := queue.Publish(fmt.Sprintf("multi-d%d", i))
		assert.NoError(t, err)
	}
	eventuallyReady(t, queue, 20)
	eventuallyUnacked(t, queue, 0)

	assert.NoError(t, queue.StartConsuming(10, time.Millisecond))

	// Assert that eventually the ready count drops to 10 and unacked rises to 10
	// TODO use the util funcs instead
	assert.Eventually(t, func() bool {
		readyCount, err := queue.readyCount()
		if err != nil {
			return false
		}
		unackedCount, err := queue.unackedCount()
		if err != nil {
			return false
		}
		return readyCount == 10 && unackedCount == 10
	}, 10*time.Second, 2*time.Millisecond)

	consumer := NewTestConsumer("multi-cons")
	consumer.AutoAck = false
	consumer.AutoFinish = false

	_, err = queue.AddConsumer("multi-cons", consumer)
	assert.NoError(t, err)

	// After we add the consumer - ready and unacked do not change
	eventuallyReady(t, queue, 10)
	eventuallyUnacked(t, queue, 10)

	assert.NoError(t, consumer.LastDelivery.Ack())
	// Assert that after the consumer acks a message the ready count drops to 9 and unacked remains at 10
	// TODO use util funcs instead
	assert.Eventually(t, func() bool {
		readyCount, err := queue.readyCount()
		if err != nil {
			return false
		}
		unackedCount, err := queue.unackedCount()
		if err != nil {
			return false
		}
		return readyCount == 9 && unackedCount == 10
	}, 10*time.Second, 2*time.Millisecond)

	consumer.Finish()
	// Assert that after the consumer finishes processing the first message ready and unacked do not change
	eventuallyReady(t, queue, 9)
	eventuallyUnacked(t, queue, 10)

	assert.NoError(t, consumer.LastDelivery.Ack())
	// Assert that after the consumer acks a message the ready count drops to 8 and unacked remains at 10
	// TODO use the util funcs instead
	assert.Eventually(t, func() bool {
		readyCount, err := queue.readyCount()
		if err != nil {
			return false
		}
		unackedCount, err := queue.unackedCount()
		if err != nil {
			return false
		}
		return readyCount == 8 && unackedCount == 10
	}, 10*time.Second, 2*time.Millisecond)

	consumer.Finish()
	// Assert that after the consumer finishes processing the second message ready and unacked do not change
	eventuallyReady(t, queue, 8)
	eventuallyUnacked(t, queue, 10)

	// This prevents the consumer from blocking internally inside a call to Consume, which allows the queue to complete
	// the call to StopConsuming
	consumer.FinishAll()

	<-queue.StopConsuming()
	assert.NoError(t, connection.stopHeartbeat())
}

func TestClusterBatch(t *testing.T) {
	connection, err := OpenConnectionWithRedisClusterClient("q1", redis.NewClusterClient(clusterOp), nil)
	assert.NoError(t, err)
	queue, err := connection.OpenQueue("batch-q")
	assert.NoError(t, err)
	_, err = queue.PurgeRejected()
	assert.NoError(t, err)
	_, err = queue.PurgeReady()
	assert.NoError(t, err)

	for i := 0; i < 5; i++ {
		err := queue.Publish(fmt.Sprintf("batch-d%d", i))
		assert.NoError(t, err)
	}

	assert.NoError(t, queue.StartConsuming(10, time.Millisecond))
	eventuallyUnacked(t, queue, 5)

	consumer := NewTestBatchConsumer()
	_, err = queue.AddBatchConsumer("batch-cons", 2, 50*time.Millisecond, consumer)
	assert.NoError(t, err)
	assert.Eventually(t, func() bool {
		return len(consumer.LastBatch) == 2
	}, 10*time.Second, 2*time.Millisecond)
	assert.Equal(t, "batch-d0", consumer.LastBatch[0].Payload())
	assert.Equal(t, "batch-d1", consumer.LastBatch[1].Payload())
	assert.NoError(t, consumer.LastBatch[0].Reject())
	assert.NoError(t, consumer.LastBatch[1].Ack())
	eventuallyUnacked(t, queue, 3)
	eventuallyRejected(t, queue, 1)

	consumer.Finish()
	assert.Eventually(t, func() bool {
		return len(consumer.LastBatch) == 2
	}, 10*time.Second, 2*time.Millisecond)
	assert.Equal(t, "batch-d2", consumer.LastBatch[0].Payload())
	assert.Equal(t, "batch-d3", consumer.LastBatch[1].Payload())
	assert.NoError(t, consumer.LastBatch[0].Reject())
	assert.NoError(t, consumer.LastBatch[1].Ack())
	eventuallyUnacked(t, queue, 1)
	eventuallyRejected(t, queue, 2)

	consumer.Finish()
	// Last Batch is cleared out
	assert.Len(t, consumer.LastBatch, 0)
	eventuallyUnacked(t, queue, 1)
	eventuallyRejected(t, queue, 2)

	// After a pause the batch consumer will pull down another batch
	assert.Eventually(t, func() bool {
		return len(consumer.LastBatch) == 1
	}, 10*time.Second, 2*time.Millisecond)
	assert.Equal(t, "batch-d4", consumer.LastBatch[0].Payload())
	assert.NoError(t, consumer.LastBatch[0].Reject())
	eventuallyUnacked(t, queue, 0)
	eventuallyRejected(t, queue, 3)
}

func TestClusterReturnRejected(t *testing.T) {
	connection, err := OpenConnectionWithRedisClusterClient("q1", redis.NewClusterClient(clusterOp), nil)
	assert.NoError(t, err)
	queue, err := connection.OpenQueue("return-q")
	assert.NoError(t, err)
	_, err = queue.PurgeReady()
	assert.NoError(t, err)

	for i := 0; i < 6; i++ {
		err := queue.Publish(fmt.Sprintf("return-d%d", i))
		assert.NoError(t, err)
	}

	eventuallyReady(t, queue, 6)
	eventuallyUnacked(t, queue, 0)
	eventuallyRejected(t, queue, 0)

	assert.NoError(t, queue.StartConsuming(10, time.Millisecond))
	eventuallyReady(t, queue, 0)
	eventuallyUnacked(t, queue, 6)
	eventuallyRejected(t, queue, 0)

	consumer := NewTestConsumer("return-cons")
	consumer.AutoAck = false
	_, err = queue.AddConsumer("cons", consumer)
	assert.NoError(t, err)
	eventuallyReady(t, queue, 0)
	eventuallyUnacked(t, queue, 6)
	eventuallyRejected(t, queue, 0)

	assert.Len(t, consumer.LastDeliveries, 6)
	assert.NoError(t, consumer.LastDeliveries[0].Reject())
	assert.NoError(t, consumer.LastDeliveries[1].Ack())
	assert.NoError(t, consumer.LastDeliveries[2].Reject())
	assert.NoError(t, consumer.LastDeliveries[3].Reject())
	// delivery 4 still open
	assert.NoError(t, consumer.LastDeliveries[5].Reject())

	eventuallyReady(t, queue, 0)
	eventuallyUnacked(t, queue, 1)  // delivery 4
	eventuallyRejected(t, queue, 4) // delivery 0, 2, 3, 5

	<-queue.StopConsuming()

	n, err := queue.ReturnRejected(2)
	assert.NoError(t, err)
	assert.Equal(t, int64(2), n)
	eventuallyReady(t, queue, 2)
	eventuallyUnacked(t, queue, 1)  // delivery 4
	eventuallyRejected(t, queue, 2) // delivery 3, 5

	n, err = queue.ReturnRejected(math.MaxInt64)
	assert.NoError(t, err)
	assert.Equal(t, int64(2), n)
	eventuallyReady(t, queue, 4)
	eventuallyUnacked(t, queue, 1) // delivery 4
	eventuallyRejected(t, queue, 0)
}

func TestClusterPushQueue(t *testing.T) {
	connection, err := OpenConnectionWithRedisClusterClient("q1", redis.NewClusterClient(clusterOp), nil)
	assert.NoError(t, err)
	queue1, err := connection.OpenQueue("queue1")
	assert.NoError(t, err)
	queue2, err := connection.OpenQueue("queue2")
	assert.NoError(t, err)
	queue1.SetPushQueue(queue2)
	assert.Equal(t, queue2.(*redisQueue).readyKey, queue1.(*redisQueue).pushKey)

	consumer1 := NewTestConsumer("push-cons")
	consumer1.AutoAck = false
	consumer1.AutoFinish = false
	assert.NoError(t, queue1.StartConsuming(10, time.Millisecond))
	_, err = queue1.AddConsumer("push-cons", consumer1)
	assert.NoError(t, err)

	consumer2 := NewTestConsumer("push-cons")
	consumer2.AutoAck = false
	consumer2.AutoFinish = false
	assert.NoError(t, queue2.StartConsuming(10, time.Millisecond))
	_, err = queue2.AddConsumer("push-cons", consumer2)
	assert.NoError(t, err)

	assert.NoError(t, queue1.Publish("d1"))
	eventuallyUnacked(t, queue1, 1)
	require.Len(t, consumer1.LastDeliveries, 1)

	assert.NoError(t, consumer1.LastDelivery.Push())
	eventuallyUnacked(t, queue1, 0)
	eventuallyUnacked(t, queue2, 1)
	require.Len(t, consumer2.LastDeliveries, 1)

	assert.NoError(t, consumer2.LastDelivery.Push())
	eventuallyRejected(t, queue2, 1)
}

func TestClusterStopConsuming_Consumer(t *testing.T) {
	connection, err := OpenConnectionWithRedisClusterClient("q1", redis.NewClusterClient(clusterOp), nil)
	assert.NoError(t, err)
	queue, err := connection.OpenQueue("consume-q")
	assert.NoError(t, err)
	_, err = queue.PurgeReady()
	assert.NoError(t, err)

	deliveryCount := int64(30)

	for i := int64(0); i < deliveryCount; i++ {
		err := queue.Publish("d" + strconv.FormatInt(i, 10))
		assert.NoError(t, err)
	}

	assert.NoError(t, queue.StartConsuming(20, time.Millisecond))

	var consumers []*TestConsumer
	for i := 0; i < 10; i++ {
		consumer := NewTestConsumer("c" + strconv.Itoa(i))
		consumers = append(consumers, consumer)
		_, err = queue.AddConsumer("consume", consumer)
		assert.NoError(t, err)
	}

	finishedChan := queue.StopConsuming()
	require.NotNil(t, finishedChan)
	<-finishedChan // wait for stopping to finish

	var consumedCount int64
	for i := 0; i < 10; i++ {
		consumedCount += int64(len(consumers[i].LastDeliveries))
	}

	// make sure all deliveries are either ready, unacked or consumed (acked)
	assert.Eventually(t, func() bool {
		readyCount, err := queue.readyCount()
		if err != nil {
			return false
		}
		unackedCount, err := queue.unackedCount()
		if err != nil {
			return false
		}
		return readyCount+unackedCount+consumedCount == deliveryCount
	}, 10*time.Second, 2*time.Millisecond)

	assert.NoError(t, connection.stopHeartbeat())
}

func TestClusterStopConsuming_BatchConsumer(t *testing.T) {
	connection, err := OpenConnectionWithRedisClusterClient("q1", redis.NewClusterClient(clusterOp), nil)
	assert.NoError(t, err)
	queue, err := connection.OpenQueue("batchConsume-q")
	assert.NoError(t, err)
	_, err = queue.PurgeReady()
	assert.NoError(t, err)

	deliveryCount := int64(50)

	for i := int64(0); i < deliveryCount; i++ {
		err := queue.Publish("d" + strconv.FormatInt(i, 10))
		assert.NoError(t, err)
	}

	assert.NoError(t, queue.StartConsuming(20, time.Millisecond))

	var consumers []*TestBatchConsumer
	for i := 0; i < 10; i++ {
		consumer := NewTestBatchConsumer()
		consumer.AutoFinish = true
		consumers = append(consumers, consumer)
		_, err = queue.AddBatchConsumer("consume", 5, time.Second, consumer)
		assert.NoError(t, err)
	}

	finishedChan := queue.StopConsuming()
	require.NotNil(t, finishedChan)
	<-finishedChan // wait for stopping to finish

	var consumedCount int64
	for i := 0; i < 10; i++ {
		consumedCount += consumers[i].ConsumedCount
	}

	// make sure all deliveries are either ready, unacked or consumed (acked)
	assert.Eventually(t, func() bool {
		readyCount, err := queue.readyCount()
		if err != nil {
			return false
		}
		unackedCount, err := queue.unackedCount()
		if err != nil {
			return false
		}
		return readyCount+unackedCount+consumedCount == deliveryCount
	}, 10*time.Second, 2*time.Millisecond)

	assert.NoError(t, connection.stopHeartbeat())
}

func TestClusterConnection_StopAllConsuming_CantOpenQueue(t *testing.T) {
	connection, err := OpenConnectionWithRedisClusterClient("q1", redis.NewClusterClient(clusterOp), nil)
	assert.NoError(t, err)

	finishedChan := connection.StopAllConsuming()
	require.NotNil(t, finishedChan)
	<-finishedChan // wait for stopping to finish

	queue, err := connection.OpenQueue("consume-q")
	require.Nil(t, queue)
	require.Equal(t, ErrorConsumingStopped, err)
}

func TestClusterConnection_StopAllConsuming_CantStartConsuming(t *testing.T) {
	connection, err := OpenConnectionWithRedisClusterClient("q1", redis.NewClusterClient(clusterOp), nil)
	assert.NoError(t, err)
	queue, err := connection.OpenQueue("consume-q")
	assert.NoError(t, err)
	_, err = queue.PurgeReady()
	assert.NoError(t, err)

	finishedChan := connection.StopAllConsuming()
	require.NotNil(t, finishedChan)
	<-finishedChan // wait for stopping to finish

	err = queue.StartConsuming(20, time.Millisecond)
	require.Equal(t, ErrorConsumingStopped, err)
}

func TestClusterQueue_StopConsuming_CantStartConsuming(t *testing.T) {
	connection, err := OpenConnectionWithRedisClusterClient("q1", redis.NewClusterClient(clusterOp), nil)
	assert.NoError(t, err)
	queue, err := connection.OpenQueue("consume-q")
	assert.NoError(t, err)
	_, err = queue.PurgeReady()
	assert.NoError(t, err)

	finishedChan := queue.StopConsuming()
	require.NotNil(t, finishedChan)
	<-finishedChan // wait for stopping to finish

	err = queue.StartConsuming(20, time.Millisecond)
	require.Equal(t, ErrorConsumingStopped, err)
}

func TestClusterConnection_StopAllConsuming_CantAddConsumer(t *testing.T) {
	connection, err := OpenConnectionWithRedisClusterClient("q1", redis.NewClusterClient(clusterOp), nil)
	assert.NoError(t, err)
	queue, err := connection.OpenQueue("consume-q")
	assert.NoError(t, err)
	_, err = queue.PurgeReady()
	assert.NoError(t, err)

	assert.NoError(t, queue.StartConsuming(20, time.Millisecond))

	finishedChan := connection.StopAllConsuming()
	require.NotNil(t, finishedChan)
	<-finishedChan // wait for stopping to finish

	_, err = queue.AddConsumer("late-consume", NewTestConsumer("late-consumer"))
	require.Equal(t, ErrorConsumingStopped, err)
}

func TestClusterQueue_StopConsuming_CantAddConsumer(t *testing.T) {
	connection, err := OpenConnectionWithRedisClusterClient("q1", redis.NewClusterClient(clusterOp), nil)
	assert.NoError(t, err)
	queue, err := connection.OpenQueue("consume-q")
	assert.NoError(t, err)
	_, err = queue.PurgeReady()
	assert.NoError(t, err)

	assert.NoError(t, queue.StartConsuming(20, time.Millisecond))

	finishedChan := queue.StopConsuming()
	require.NotNil(t, finishedChan)
	<-finishedChan // wait for stopping to finish

	_, err = queue.AddConsumer("late-consume", NewTestConsumer("late-consumer"))
	require.Equal(t, ErrorConsumingStopped, err)
}

func BenchmarkClusterQueue(b *testing.B) {
	// open queue
	connection, err := OpenConnectionWithRedisClusterClient("q1", redis.NewClusterClient(clusterOp), nil)
	assert.NoError(b, err)
	queueName := fmt.Sprintf("bench-q%d", b.N)
	queue, err := connection.OpenQueue(queueName)
	assert.NoError(b, err)
	assert.NoError(b, queue.StartConsuming(10, time.Millisecond))

	// add some consumers
	numConsumers := 10
	var consumers []*TestConsumer
	for i := 0; i < numConsumers; i++ {
		consumer := NewTestConsumer("bench-A")
		// consumer.SleepDuration = time.Microsecond
		consumers = append(consumers, consumer)
		_, err = queue.AddConsumer("bench-cons", consumer)
		assert.NoError(b, err)
	}

	// publish deliveries
	for i := 0; i < b.N; i++ {
		err := queue.Publish("bench-d")
		assert.NoError(b, err)
	}

	// wait until all are consumed
	for {
		ready, err := queue.readyCount()
		assert.NoError(b, err)
		unacked, err := queue.unackedCount()
		assert.NoError(b, err)
		fmt.Printf("%d unacked %d %d\n", b.N, ready, unacked)
		if ready == 0 && unacked == 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}

	time.Sleep(time.Millisecond)

	sum := 0
	for _, consumer := range consumers {
		sum += len(consumer.LastDeliveries)
	}
	fmt.Printf("consumed %d\n", sum)

	assert.NoError(b, connection.stopHeartbeat())
}

func TestCluster_jitteredDuration(t *testing.T) {
	dur := 100 * time.Millisecond
	for i := 0; i < 5000; i++ {
		d := jitteredDuration(dur)
		assert.LessOrEqual(t, int64(90*time.Millisecond), int64(d))
		assert.GreaterOrEqual(t, int64(110*time.Millisecond), int64(d))
	}
}

func TestClusterQueueDrain(t *testing.T) {
	connection, err := OpenConnectionWithRedisClusterClient("q1", redis.NewClusterClient(clusterOp), nil)
	assert.NoError(t, err)
	require.NotNil(t, connection)

	queue, err := connection.OpenQueue("drain-queue")
	assert.NoError(t, err)

	for x := 0; x < 100; x++ {
		queue.Publish(fmt.Sprintf("%d", x))
	}

	eventuallyReady(t, queue, 100)

	for x := 1; x <= 10; x++ {
		values, err := queue.Drain(10)
		assert.NoError(t, err)
		assert.Equal(t, 10, len(values))
		eventuallyReady(t, queue, int64(100-x*10))
	}
}
