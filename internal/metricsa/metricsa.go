package metricsa

import (
	"github.com/prometheus/client_golang/prometheus"
	ctrlMetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

//go:generate mockery --name=Metrics
type Metrics interface {
	SetDefaultShoot()
	SetFallbackShoot()
}

type metricsImpl struct {
	shootsDefault  prometheus.Counter
	shootsFallback prometheus.Counter
}

func (m metricsImpl) SetDefaultShoot() {
	//m.shootsDefault.DeletePartialMatch(prometheus.Labels{shootNameIDKeyName: shootName})
	m.shootsDefault.Inc()
}

func (m metricsImpl) SetFallbackShoot() {
	//m.shootsFallback.DeletePartialMatch(prometheus.Labels{shootNameIDKeyName: shootName})
	m.shootsFallback.Inc()
}

//func (m metricsImpl) CleanUpCounters(runtimeID, runtimeName string) {
//	m.shootsDefault.
//	//m.runtimeStateGauge.Delete(prometheus.Labels{runtimeIDKeyName: runtimeID, runtimeNameKeyName: runtimeName})
//}

func NewMetrics() Metrics {
	m := &metricsImpl{
		shootsDefault: prometheus.NewCounter(
			prometheus.CounterOpts{
				Subsystem: "kim_snatch",
				Name:      "snatch_shoots_default",
				Help:      "Indicates the number of Shoots with NodeAffinity",
			}),
		shootsFallback: prometheus.NewCounter(
			prometheus.CounterOpts{
				Subsystem: "kim_snatch",
				Name:      "snatch_shoots_fallback",
				Help:      "Indicates the number of Shoots with missing NodeAffinity",
			}),
	}
	ctrlMetrics.Registry.MustRegister(m.shootsDefault, m.shootsFallback)
	return m
}
