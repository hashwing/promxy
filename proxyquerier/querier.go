package proxyquerier

import (
	"context"
	"time"

	"github.com/hashwing/promxy/config"
	"github.com/hashwing/promxy/promclient"
	"github.com/hashwing/promxy/servergroup"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/pkg/timestamp"
	"github.com/prometheus/prometheus/storage"
	"github.com/sirupsen/logrus"
)

var (
	proxyQuerierSummary = prometheus.NewSummaryVec(prometheus.SummaryOpts{
		Name: "proxy_querier_request",
		Help: "Summary of proxyquerier calls to downstreams",
	}, []string{"host", "call", "status"})
)

func init() {
	prometheus.MustRegister(proxyQuerierSummary)
}

type ProxyQuerier struct {
	Ctx          context.Context
	Start        time.Time
	End          time.Time
	ServerGroups servergroup.ServerGroups

	Cfg *proxyconfig.PromxyConfig
}

// Select returns a set of series that matches the given label matchers.
func (h *ProxyQuerier) Select(selectParams *storage.SelectParams, matchers ...*labels.Matcher) (storage.SeriesSet, error) {
	start := time.Now()
	defer func() {
		logrus.WithFields(logrus.Fields{
			"selectParams": selectParams,
			"matchers":     matchers,
			"took":         time.Now().Sub(start),
		}).Debug("Select")
	}()
    // add common labels if define
	names,ok:=h.Ctx.Value("common_labels").(map[string]string)
	if ok {
		for k,v:=range names{
			matcher:=&labels.Matcher{
				Type: labels.MatchEqual,
				Name: k,
				Value: v,
			}
			matchers = append(matchers,matcher)
		}
	}

	var result model.Value
	var err error
	// Select() is a combined API call for query/query_range/series.
	// as of right now there is no great way of differentiating between a
	// data call (query/query_range) and a metadata call (series). For now
	// the working workaround is to switch based on the selectParams.
	// https://github.com/prometheus/prometheus/issues/4057
	if selectParams == nil {
		result, err = h.ServerGroups.GetSeries(h.Ctx, h.Start, h.End, matchers)
	} else {
		result, err = h.ServerGroups.GetValue(h.Ctx, timestamp.Time(selectParams.Start), timestamp.Time(selectParams.End), matchers)
	}
	if err != nil {
		return nil, err
	}

	iterators := promclient.IteratorsForValue(result)

	series := make([]storage.Series, len(iterators))
	for i, iterator := range iterators {
		series[i] = &Series{iterator}
	}

	return NewSeriesSet(series), nil
}

// LabelValues returns all potential values for a label name.
func (h *ProxyQuerier) LabelValues(name string) ([]string, error) {
	start := time.Now()
	defer func() {
		logrus.WithFields(logrus.Fields{
			"name": name,
			"took": time.Now().Sub(start),
		}).Debug("LabelValues")
	}()

	result, err := h.ServerGroups.GetValuesForLabelName(h.Ctx, "/api/v1/label/"+string(name)+"/values")
	if err != nil {
		return nil, err
	}

	ret := make([]string, len(result))
	for i, r := range result {
		ret[i] = string(r)
	}

	return ret, nil
}

// Close closes the querier. Behavior for subsequent calls to Querier methods
// is undefined.
func (h *ProxyQuerier) Close() error { return nil }
