package main

import (
 "encoding/json"
 "fmt"
 "math"
 "testing"
 "time"
)

// Run beside an unchanged copy of protocols/christian/clock.go.
// No Sync, step, host clock change, or artificial drift is involved.
func TestZeroClockLiveMeasurement(t *testing.T) {
 anchor := time.Now()
 c := newClock(anchor, 0, 0)
 ticker := time.NewTicker(100 * time.Millisecond)
 defer ticker.Stop()
 fmt.Printf("ENV anchor=%s floatSpacingMs=%.12f\n", anchor.String(), math.Nextafter(c.baseMs, math.Inf(1))-c.baseMs)
 for i:=0; i<=150; i++ {
  if i>0 { <-ticker.C }
  now := time.Now()
  mono := now.Sub(anchor)
  wall := now.Round(0).Sub(anchor.Round(0)) // strip monotonic ONLY for the control measurement
  errorMs := c.errorMs(now)
  differenceMs := float64(mono-wall)/float64(time.Millisecond)
  // Integer microsecond truncation plus float conversion, using exactly the
  // expression in clock.go, but substituting wall elapsed for monotonic elapsed.
  wallOnlyErrorMs := c.baseMs + float64(wall.Microseconds())/1000-realMs(now)
  row:=struct{
   ElapsedSeconds float64 `json:"elapsedSeconds"`
   ErrorMs float64 `json:"errorMs"`
   DisplayedErrorMs float64 `json:"displayedErrorMs"`
   MonotonicMinusWallMs float64 `json:"monotonicMinusWallMs"`
   ResidualMs float64 `json:"residualMs"`
   WallOnlyErrorMs float64 `json:"wallOnlyErrorMs"`
  }{mono.Seconds(),errorMs,round1(errorMs),differenceMs,errorMs-differenceMs,wallOnlyErrorMs}
  data,err:=json.Marshal(row); if err!=nil{t.Fatal(err)}
  fmt.Println("SAMPLE",string(data))
 }
}

// An ideal clock pair: equal elapsed times, but fractions are truncated
// independently. A nonzero error here cannot come from OS clock differences.
func TestZeroClockTruncationControl(t *testing.T){
 anchor:=time.Unix(1790000000,900)
 now:=anchor.Add(200*time.Nanosecond)
 c:=newClock(anchor,0,0)
 if c.errorMs(now)==0 {t.Fatal("expected sub-microsecond truncation to be visible")}
 fmt.Printf("CONTROL elapsedNs=200 wallElapsedNs=200 errorMs=%.12f displayedErrorMs=%.1f\n", c.errorMs(now),round1(c.errorMs(now)))
}
