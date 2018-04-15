package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/Luzifer/rconfig"
	"github.com/elastic/beats/libbeat/common/dtfmt"
	"github.com/olivere/elastic"
	"github.com/robfig/cron"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/context"
	elogrus "gopkg.in/sohlich/elogrus.v3"
	"gopkg.in/yaml.v2"
)

var (
	cfg = struct {
		ConfigFile string        `flag:"config" default:"config.yaml" description:"Cron definition file"`
		Hostname   string        `flag:"hostname" description:"Overwrite system hostname"`
		PingTimout time.Duration `flag:"ping-timeout" default:"1s" description:"Timeout for success / failure pings"`
	}{}

	version = "dev"
)

type cronConfig struct {
	Elasticsearch struct {
		Auth    []string `yaml:"auth"`
		Index   string   `yaml:"index"`
		Servers []string `yaml:"servers"`
	} `yaml:"elasticsearch"`
	Jobs []cronJob `yaml:"jobs"`
}

type cronJob struct {
	Name        string   `yaml:"name"`
	Schedule    string   `yaml:"schedule"`
	Command     string   `yaml:"cmd"`
	Arguments   []string `yaml:"args"`
	PingSuccess string   `yaml:"ping_success"`
	PingFailure string   `yaml:"ping_failure"`
}

func init() {
	rconfig.ParseAndValidate(&cfg)

	if cfg.Hostname == "" {
		hostname, _ := os.Hostname()
		cfg.Hostname = hostname
	}
}

func readConfig() (*cronConfig, error) {
	fp, err := os.Open(cfg.ConfigFile)
	if err != nil {
		return nil, err
	}
	defer fp.Close()

	cc := &cronConfig{}
	cc.Elasticsearch.Index = "elastic_cron-%{+YYYY.MM.dd}"
	return cc, yaml.NewDecoder(fp).Decode(cc)
}

func main() {
	cc, err := readConfig()
	if err != nil {
		log.Fatalf("Unable to read config file: %s", err)
	}

	c := cron.New()

	for i := range cc.Jobs {
		job := cc.Jobs[i]
		if err := c.AddFunc(job.Schedule, getJobExecutor(job)); err != nil {
			log.Fatalf("Unable to add job '%s': %s", job.Name, err)
		}
	}

	opts := []elastic.ClientOptionFunc{
		elastic.SetURL(cc.Elasticsearch.Servers...),
	}

	if cc.Elasticsearch.Auth != nil && len(cc.Elasticsearch.Auth) == 2 && cc.Elasticsearch.Auth[0] != "" {
		opts = append(opts, elastic.SetBasicAuth(cc.Elasticsearch.Auth[0], cc.Elasticsearch.Auth[1]))
	}

	esClient, err := elastic.NewSimpleClient(opts...)
	if err != nil {
		log.WithError(err).Fatal("Unable to create elasticsearch client")
	}

	hook, err := elogrus.NewElasticHookWithFunc(esClient, cfg.Hostname, log.InfoLevel, getIndexNameFunc(cc))
	if err != nil {
		log.WithError(err).Fatal("Unable to create elasticsearch log hook")
	}
	log.AddHook(hook)

	c.Run()
}

func getIndexNameFunc(cc *cronConfig) func() string {
	if !strings.Contains(cc.Elasticsearch.Index, `%{+`) {
		// Simple string without date expansion
		return func() string { return cc.Elasticsearch.Index }
	}

	return func() string {
		rex := regexp.MustCompile(`%{\+([^}]+)}`)
		return rex.ReplaceAllStringFunc(cc.Elasticsearch.Index, func(f string) string {
			f = strings.TrimSuffix(strings.TrimPrefix(f, `%{+`), `}`)
			d, _ := dtfmt.Format(time.Now(), f)
			return d
		})
	}
}

func getJobExecutor(job cronJob) func() {
	return func() {
		logger := log.WithFields(log.Fields{
			"job": job.Name,
		})

		stdout := logger.WriterLevel(log.InfoLevel)
		defer stdout.Close()
		stderr := logger.WriterLevel(log.ErrorLevel)
		defer stderr.Close()

		fmt.Fprintln(stdout, "[SYS] Starting job")

		cmd := exec.Command(job.Command, job.Arguments...)
		cmd.Stdout = stdout
		cmd.Stderr = stderr

		err := cmd.Run()
		switch err.(type) {
		case nil:
			logger.Info("[SYS] Command execution successful")
			go func(url string) {
				if err := doPing(url); err != nil {
					logger.WithError(err).Errorf("[SYS] Ping to URL %q caused an error", url)
				}
			}(job.PingSuccess)

		case *exec.ExitError:
			logger.Info("[SYS] Command exited with unexpected exit code != 0")
			go func(url string) {
				if err := doPing(url); err != nil {
					logger.WithError(err).Errorf("[SYS] Ping to URL %q caused an error", url)
				}
			}(job.PingFailure)

		default:
			logger.WithError(err).Error("[SYS] Execution caused error")
			go func(url string) {
				if err := doPing(url); err != nil {
					logger.WithError(err).Errorf("[SYS] Ping to URL %q caused an error", url)
				}
			}(job.PingFailure)

		}
	}
}

func doPing(url string) error {
	if url == "" {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.PingTimout)
	defer cancel()

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		return err
	}

	if resp.StatusCode > 299 {
		return fmt.Errorf("Expected HTTP2xx status, got HTTP%d", resp.StatusCode)
	}

	return nil
}
