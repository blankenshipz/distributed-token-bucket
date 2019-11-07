# DTB (Distributed Token Bucket)

This library provides a [token bucket](https://en.wikipedia.org/wiki/Token_bucket)
that works in a distributed way and without relying on lua scripts in Redis.

## Description
The bucket is maintained in Redis - each client attempts to acquire an
expiring lock and add tokens to the bucket. If the lock does expire (meaning
that the owner has likely perished) then the next client to visit the lock
acquires it. Currently the locks expire after a duration of twice the token
cadence has elapsed and to prevent expiration the client that owns the lock
pushes the expiration forward on each addition of tokens.

## Usage
Here's a small example of using this in practice:

``` golang
func main() {
  redisClient := redis.NewClient(
    &redis.Options{
      Addr:     "redis:6379",
      Password: "", // no password set
      DB:       0,  // use default DB
    },
  )

  dtb := NewDTB(
    "my-token-bucket",                 // the name of the bucket
    1000,                              // the capacity
    time.Duration(1*time.Second),      // the cadence to add tokens
    redisClient                        // the redis client
  )

  for {
    fmt.Println("--------------------------------")
    // blocking call to "get" a token
    err := dtb.GetToken()

    if err != nil {
      fmt.Fatal(err)
    }
    fmt.Println("Got a token!")
    time.Sleep(time.Duration(500 * time.Millisecond))
    fmt.Println("--------------------------------")
  }
}
```

TODO:
  - [ ] Add `#Shutdown` for nicely exiting the goroutine of the "maintainer"
  - [ ] Add some smart catchup features if possible
  - [ ] Consider making the lockDuration configurable

