package promxy

import (
	"context"
	"encoding/json"

	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "net/http/pprof"

	"crypto/md5"

	kitlog "github.com/go-kit/kit/log"
	"github.com/hashwing/promxy/config"
	"github.com/hashwing/promxy/proxystorage"
	"github.com/julienschmidt/httprouter"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/promlog"
	"github.com/prometheus/common/route"
	"github.com/prometheus/common/version"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/discovery"
	sd_config "github.com/prometheus/prometheus/discovery/config"
	"github.com/prometheus/prometheus/notifier"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/rules"
	"github.com/prometheus/prometheus/scrape"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/util/strutil"
	"github.com/prometheus/prometheus/web"
	"github.com/sirupsen/logrus"
)

var (
	reloadTime = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "process_reload_time_seconds",
		Help: "Last reload (SIGHUP) time of the process since unix epoch in seconds.",
	})
	reloadables = make([]proxyconfig.Reloadable, 0)
)

func ReloadConfig(cfg *proxyconfig.Config) error {
	rls := reloadables

	failed := false
	for _, rl := range rls {
		if err := rl.ApplyConfig(cfg); err != nil {
			logrus.Errorf("Failed to apply configuration: %v", err)
			failed = true
		}
	}

	if failed {
		return fmt.Errorf("One or more errors occurred while applying new configuration")
	}
	reloadTime.Set(float64(time.Now().Unix()))
	return nil
}

// HTTPMiddle http middle as auth etc
type HTTPMiddle func(w http.ResponseWriter, r *http.Request) (*http.Request, bool)

func Run(ctx context.Context, cfg *proxyconfig.Config, nf NotifyFunc, middle HTTPMiddle) (*httprouter.Router, error) {
	// Wait for reload or termination signals. Start the handler for SIGHUP as
	// early as possible, but ignore it until we are ready to handle reloading
	// our config.
	prometheus.MustRegister(reloadTime)

	// Use log level
	level := logrus.InfoLevel
	switch strings.ToLower(cfg.PromxyConfig.LogLevel) {
	case "panic":
		level = logrus.PanicLevel
	case "fatal":
		level = logrus.FatalLevel
	case "error":
		level = logrus.ErrorLevel
	case "warn":
		level = logrus.WarnLevel
	case "info":
		level = logrus.InfoLevel
	case "debug":
		level = logrus.DebugLevel
	default:
		return nil, fmt.Errorf("Unknown log level: %s", cfg.PromxyConfig.LogLevel)
	}
	logrus.SetLevel(level)

	// Set the log format to have a reasonable timestamp
	formatter := &logrus.TextFormatter{
		FullTimestamp: true,
	}
	logrus.SetFormatter(formatter)

	// Reload ready -- channel to close once we are ready to start reloaders
	reloadReady := make(chan struct{}, 0)

	// Create the proxy storag
	var proxyStorage storage.Storage

	ps, err := proxystorage.NewProxyStorage()
	if err != nil {
		return nil, err
	}
	reloadables = append(reloadables, ps)
	proxyStorage = ps

	engine := promql.NewEngine(nil, prometheus.DefaultRegisterer, cfg.PromxyConfig.QueryMaxConcurrency, time.Duration(int64(cfg.PromxyConfig.QueryTimeout))*time.Second)
	engine.NodeReplacer = ps.NodeReplacer

	// TODO: rename
	externalUrl, err := computeExternalURL(cfg.PromxyConfig.ExternalURL, cfg.PromxyConfig.BindAddr)
	if err != nil {
		return nil, err
	}

	// Alert notifier
	lvl := promlog.AllowedLevel{}
	if err := lvl.Set("info"); err != nil {
		panic(err)
	}
	logger := promlog.New(lvl)

	notifierManager := notifier.NewManager(
		&notifier.Options{
			Registerer:    prometheus.DefaultRegisterer,
			QueueCapacity: cfg.PromxyConfig.NotificationQueueCapacity,
		},
		kitlog.With(logger, "component", "notifier"),
	)
	reloadables = append(reloadables, proxyconfig.WrapPromReloadable(notifierManager))

	discoveryManagerNotify := discovery.NewManager(ctx, kitlog.With(logger, "component", "discovery manager notify"))
	reloadables = append(reloadables,
		proxyconfig.WrapPromReloadable(&proxyconfig.ApplyConfigFunc{func(cfg *config.Config) error {
			c := make(map[string]sd_config.ServiceDiscoveryConfig)
			for _, v := range cfg.AlertingConfig.AlertmanagerConfigs {
				// AlertmanagerConfigs doesn't hold an unique identifier so we use the config hash as the identifier.
				b, err := json.Marshal(v)
				if err != nil {
					return err
				}
				c[fmt.Sprintf("%x", md5.Sum(b))] = v.ServiceDiscoveryConfig
			}
			return discoveryManagerNotify.ApplyConfig(c)
		}}),
	)

	go func() {
		if err := discoveryManagerNotify.Run(); err != nil {
			logrus.Errorf("Error running Notify discovery manager: %v", err)
		} else {
			logrus.Infof("Notify discovery manager stopped")
		}
	}()
	go func() {
		<-reloadReady
		notifierManager.Run(discoveryManagerNotify.SyncCh())
		logrus.Infof("Notifier manager stopped")
	}()

	ruleManager := rules.NewManager(&rules.ManagerOptions{
		Context:     ctx,         // base context for all background tasks
		ExternalURL: externalUrl, // URL listed as URL for "who fired this alert"
		QueryFunc:   rules.EngineQueryFunc(engine, proxyStorage),
		NotifyFunc:  sendAlerts(notifierManager, externalUrl.String(), nf),
		Appendable:  proxyStorage,
		Logger:      logger,
	})
	go ruleManager.Run()

	reloadables = append(reloadables, proxyconfig.WrapPromReloadable(&proxyconfig.ApplyConfigFunc{func(cfg *config.Config) error {
		// Get all rule files matching the configuration oaths.
		var files []string
		for _, pat := range cfg.RuleFiles {
			fs, err := filepath.Glob(pat)
			if err != nil {
				// The only error can be a bad pattern.
				return fmt.Errorf("error retrieving rule files for %s: %s", pat, err)
			}
			files = append(files, fs...)
		}
		return ruleManager.Update(time.Duration(cfg.GlobalConfig.EvaluationInterval), files)
	}}))

	// We need an empty scrape manager, simply to make the API not panic and error out
	scrapeManager := scrape.NewManager(kitlog.With(logger, "component", "scrape manager"), nil)

	// TODO: separate package?
	webOptions := &web.Options{
		Context:       ctx,
		Storage:       proxyStorage,
		QueryEngine:   engine,
		ScrapeManager: scrapeManager,
		RuleManager:   ruleManager,
		Notifier:      notifierManager,

		RoutePrefix:     "/", // TODO: options for this?
		ExternalURL:     externalUrl,
		EnableLifecycle: true,
		// TODO: use these?
		/*
			ListenAddress        string
			ReadTimeout          time.Duration
			MaxConnections       int
			MetricsPath          string
			UseLocalAssets       bool
			UserAssetsPath       string
			ConsoleTemplatesPath string
			ConsoleLibrariesPath string
			EnableLifecycle      bool
			EnableAdminAPI       bool
		*/
		Version: &web.PrometheusVersion{
			Version:   version.Version,
			Revision:  version.Revision,
			Branch:    version.Branch,
			BuildUser: version.BuildUser,
			BuildDate: version.BuildDate,
			GoVersion: version.GoVersion,
		},
	}

	webHandler := web.New(logger, webOptions)
	reloadables = append(reloadables, proxyconfig.WrapPromReloadable(webHandler))
	webHandler.Ready()

	apiRouter := route.New()
	webHandler.Getv1API().Register(apiRouter.WithPrefix("/api/v1"))

	// Create our router
	r := httprouter.New()

	// TODO: configurable metrics path
	r.HandlerFunc("GET", "/metrics", prometheus.Handler().ServeHTTP)

	r.NotFound = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r, res := middle(w, r)
		if !res {
			return
		}
		// Have our fallback rules
		if strings.HasPrefix(r.URL.Path, "/api/") {
			apiRouter.ServeHTTP(w, r)

		} else if strings.HasPrefix(r.URL.Path, "/debug") {
			http.DefaultServeMux.ServeHTTP(w, r)
		} else {
			// all else we send direct to the local prometheus UI
			webHandler.GetRouter().ServeHTTP(w, r)
		}
	})

	if err := ReloadConfig(cfg); err != nil {
		return r, fmt.Errorf("Error loading config: %s", err)
		//logrus.Fatalf("Error loading config: %s", err)
	}

	close(reloadReady)

	// Set up access logger
	// loggedRouter := logging.NewApacheLoggingHandler(r, logging.LogToWriter(os.Stdout))
	// srv := &http.Server{
	// 	Addr:    opts.BindAddr,
	// 	Handler: loggedRouter,
	// }

	// go func() {
	// 	logrus.Infof("promxy starting")
	// 	if err := srv.ListenAndServe(); err != nil {
	// 		log.Fatalf("Error listening: %v", err)
	// 	}
	// }()
	return r, nil
}

// Alert ...
type Alert struct {
	notifier.Alert
}

// NotifyFunc implements rules.NotifyFunc
type NotifyFunc func(as ...*Alert) error

// sendAlerts implements the rules.NotifyFunc for a Notifier.
// It filters any non-firing alerts from the input.
func sendAlerts(n *notifier.Manager, externalURL string, nf NotifyFunc) rules.NotifyFunc {
	return func(ctx context.Context, expr string, alerts ...*rules.Alert) error {
		var nres []*notifier.Alert
		var res []*Alert
		for _, alert := range alerts {
			// Only send actually firing alerts.
			if alert.State == rules.StatePending {
				continue
			}
			na := &notifier.Alert{
				StartsAt:     alert.FiredAt,
				Labels:       alert.Labels,
				Annotations:  alert.Annotations,
				GeneratorURL: externalURL + strutil.TableLinkForExpression(expr),
			}

			if !alert.ResolvedAt.IsZero() {
				na.EndsAt = alert.ResolvedAt
			}
			a := &Alert{
				*na,
			}
			res = append(res, a)
			nres = append(nres, na)
		}

		if len(alerts) > 0 {
			n.Send(nres...)
			return nf(res...)
		}
		return nil
	}
}

func startsOrEndsWithQuote(s string) bool {
	return strings.HasPrefix(s, "\"") || strings.HasPrefix(s, "'") ||
		strings.HasSuffix(s, "\"") || strings.HasSuffix(s, "'")
}

// computeExternalURL computes a sanitized external URL from a raw input. It infers unset
// URL parts from the OS and the given listen address.
func computeExternalURL(u, listenAddr string) (*url.URL, error) {
	if u == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return nil, err
		}
		_, port, err := net.SplitHostPort(listenAddr)
		if err != nil {
			return nil, err
		}
		u = fmt.Sprintf("http://%s:%s/", hostname, port)
	}

	if startsOrEndsWithQuote(u) {
		return nil, fmt.Errorf("URL must not begin or end with quotes")
	}

	eu, err := url.Parse(u)
	if err != nil {
		return nil, err
	}

	ppref := strings.TrimRight(eu.Path, "/")
	if ppref != "" && !strings.HasPrefix(ppref, "/") {
		ppref = "/" + ppref
	}
	eu.Path = ppref

	return eu, nil
}
