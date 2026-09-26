// Package jobs runs background work on River. The api process inserts jobs
// through a client from NewInsertClient, inside the transaction of the write
// that needs them when there is one. The worker process works every queue
// through a client from NewWorkerClient, which logs failed attempts and
// records job metrics. River runs periodic jobs on the elected leader only,
// on schedules from Every and DailyAt.
package jobs
