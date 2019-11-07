package dtb

import (
	"fmt"
	"time"

	"github.com/go-redis/redis/v7"
)

type DTB struct {
	bucketName string
	capacity   int64
	cadence    time.Duration
	rc         *redis.Client

	errors chan error
}

// NewDTB provisions a distributed token bucket using the provided redis connection
func NewDTB(bucketName string, capacity int64, cadence time.Duration, redisClient *redis.Client) *DTB {
	errors := make(chan error)

	dtb := &DTB{
		bucketName: bucketName,
		capacity:   capacity,
		cadence:    cadence,
		rc:         redisClient,

		errors: errors,
	}

	go dtb.fill()

	return dtb
}

func (dtb *DTB) lockKey() string {
	return fmt.Sprintf("%v_lock", dtb.bucketName)
}

// lockDuration defines the amount of time we shift our hold on the lock into
// the future; we're using two times the cadence as a reasonable default.
func (dtb *DTB) lockDuration() time.Duration {
	return time.Duration(2) * dtb.cadence
}

func (dtb *DTB) acquireLock() (*int64, error) {
	now := time.Now()
	lockVal := now.Add(dtb.lockDuration()).Unix()
	lockKey := dtb.lockKey()

	acquired := dtb.rc.SetNX(lockKey, lockVal, 0)

	if acquired.Err() != nil {
		return nil, acquired.Err()
	}

	if acquired.Val() {
		return &lockVal, nil
	}

	//
	// We weren't able to acquire the lock
	//
	lockUnix, err := dtb.rc.Get(lockKey).Int64()

	if err != nil {
		return nil, err
	}

	// if the lock is expired
	if time.Unix(lockUnix, 0).Before(now) {
		// set the lock and get what was stored there
		val := dtb.rc.GetSet(lockKey, lockVal)

		if val.Err() != nil {
			return nil, val.Err()
		}

		i, err := val.Int64()

		if err != nil {
			return nil, err
		}

		// if the old value is not actually expired then we've not got the lock
		// but we did update it! this could create a weird corner case
		// when we're working with a lock that we own later we need to take this
		// into account
		if time.Unix(i, 0).After(now) { // someone else still has the lock
			return nil, nil
		} else { // someone _did_ have the lock and it's now expired
			return &lockVal, nil
		}
	} else { // lockUnix >= nowUnix
		// someone else has the lock
		return nil, nil
	}
}

// fill does the background work to ensure new tokens are being added to the bucket
func (dtb *DTB) fill() {
	var lockID *int64
	var err error

	// forever try to become the maintainer of the bucket
	for lockID == nil {
		time.Sleep(dtb.cadence)
		lockID, err = dtb.acquireLock()

		if err != nil {
			dtb.errors <- err
			return
		}
	}

	for c := time.Tick(dtb.cadence); ; <-c {
		tokensInBucket := dtb.rc.LLen(dtb.bucketName)

		if tokensInBucket.Err() != nil {
			dtb.errors <- tokensInBucket.Err()
			return
		}

		if tokensInBucket.Val() < dtb.capacity {
			val := dtb.rc.LPush(dtb.bucketName, nil)

			if val.Err() != nil {
				dtb.errors <- val.Err()
				return
			}
		}

		// update the lock to sometime further in the future so we retain it
		// the only way we lose the lock is if we die
		dtb.rc.Set(
			dtb.lockKey(),
			time.Now().Add(dtb.lockDuration()).Unix(),
			0,
		)
	}
}

// GetToken blocks until a token becomes available or the world ends.
// If an error has occured filling the bucket or connecting to redis then
// GetToken will return an error
func (dtb *DTB) GetToken() error {
	var err error

	// Check for errors
	select {
	case msg := <-dtb.errors:
		err = msg
	default:
		err = nil
	}

	if err != nil {
		return err
	}

	// Pop a token waiting until the sun begins to cool
	dtb.rc.BRPop(0, dtb.bucketName)

	return nil
}
