package indexer

import (
	"github.com/couchbase/indexing/secondary/logging"
	"github.com/couchbase/indexing/secondary/system"
	"math"
	"sync/atomic"
	"time"
)

//////////////////////////////////////////////////////////////
// Global Variable
//////////////////////////////////////////////////////////////

var cpuPercent uint64
var rss uint64
var memTotal uint64
var memFree uint64

//////////////////////////////////////////////////////////////
// Concrete Type/Struct
//////////////////////////////////////////////////////////////

type cpuCollector struct {
	stats *system.SystemStats
}

//////////////////////////////////////////////////////////////
// Cpu Collector
//////////////////////////////////////////////////////////////

//
// Start Cpu collection
//
func StartCpuCollector() error {

	collector := &cpuCollector{}

	// open sigar for stats
	stats, err := system.NewSystemStats()
	if err != nil {
		logging.Errorf("Fail to start cpu stat collector. Err=%v", err)
		return err
	}
	collector.stats = stats

	// skip the first one
	collector.stats.ProcessCpuPercent()
	collector.stats.ProcessRSS()
	collector.stats.FreeMem()
	collector.stats.TotalMem()

	// start stats collection
	go collector.runCollectStats()

	return nil
}

//
// Gather Cpu
//
func (c *cpuCollector) runCollectStats() {

	//ticker := time.NewTicker(time.Second * 60)
	ticker := time.NewTicker(time.Second * 30)
	defer ticker.Stop()

	count := 0

	for range ticker.C {

		pid, cpu, err := c.stats.ProcessCpuPercent()
		if err != nil {
			logging.Debugf("Fail to get cpu percentage. Err=%v", err)
			continue
		}
		updateCpuPercent(cpu)

		_, rss, err := c.stats.ProcessRSS()
		if err != nil {
			logging.Debugf("Fail to get RSS. Err=%v", err)
			continue
		}
		updateRSS(rss)

		total, err := c.stats.TotalMem()
		if err != nil {
			logging.Debugf("Fail to get total memory. Err=%v", err)
			continue
		}
		updateMemTotal(total)

		free, err := c.stats.FreeMem()
		if err != nil {
			logging.Debugf("Fail to get free memory. Err=%v", err)
			continue
		}
		updateMemFree(free)

		count++
		if count > 10 {
			logging.Debugf("cpuCollector: cpu percent %v for pid %v", cpu, pid)
			logging.Debugf("cpuCollector: RSS %v for pid %v", rss, pid)
			logging.Debugf("cpuCollector: memory total %v", total)
			logging.Debugf("cpuCollector: memory free %vv", free)
			count = 0
		}
	}
}

//////////////////////////////////////////////////////////////
// Global Function
//////////////////////////////////////////////////////////////

func updateCpuPercent(cpu float64) {

	atomic.StoreUint64(&cpuPercent, math.Float64bits(cpu))
}

func getCpuPercent() float64 {

	bits := atomic.LoadUint64(&cpuPercent)
	return math.Float64frombits(bits)
}

func updateRSS(mem uint64) {

	atomic.StoreUint64(&rss, mem)
}

func getRSS() uint64 {

	return atomic.LoadUint64(&rss)
}

func updateMemTotal(mem uint64) {

	atomic.StoreUint64(&memTotal, mem)
}

func getMemTotal() uint64 {

	return atomic.LoadUint64(&memTotal)
}

func updateMemFree(mem uint64) {

	atomic.StoreUint64(&memFree, mem)
}

func getMemFree() uint64 {

	return atomic.LoadUint64(&memFree)
}
