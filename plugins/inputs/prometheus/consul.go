package prometheus

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"strings"
	"text/template"
	"time"

	"github.com/hashicorp/consul/api"

	"github.com/influxdata/telegraf/config"
)

type consulConfig struct {
	// Address of the Consul agent. The address must contain a hostname or an IP address
	// and optionally a port (format: "host:port").
	Enabled       bool            `toml:"enabled"`
	Agent         string          `toml:"agent"`
	QueryInterval config.Duration `toml:"query_interval"`
	Queries       []*consulQuery  `toml:"query"`
}

// One Consul service discovery query
type consulQuery struct {
	// A name of the searched services (not ID)
	ServiceName string `toml:"name"`

	// A tag of the searched services
	ServiceTag string `toml:"tag"`

	// A DC of the searched services
	ServiceDc string `toml:"dc"`

	// A template URL of the Prometheus gathering interface. The hostname part
	// of the URL will be replaced by discovered address and port.
	ServiceURL string `toml:"url"`

	// Extra tags to add to metrics found in Consul
	ServiceExtraTags map[string]string `toml:"tags"`

	serviceURLTemplate       *template.Template
	serviceExtraTagsTemplate map[string]*template.Template

	// Store last error status and change log level depending on repeated occurrence
	lastQueryFailed bool
}

func (p *Prometheus) startConsul(ctx context.Context) error {
	consulAPIConfig := api.DefaultConfig()
	if p.ConsulConfig.Agent != "" {
		consulAPIConfig.Address = p.ConsulConfig.Agent
	}

	consul, err := api.NewClient(consulAPIConfig)
	if err != nil {
		return fmt.Errorf("cannot connect to the Consul agent: %w", err)
	}

	// Parse the template for metrics URL, drop queries with template parse errors
	i := 0
	for _, q := range p.ConsulConfig.Queries {
		serviceURLTemplate, err := template.New("URL").Parse(q.ServiceURL)
		if err != nil {
			p.Log.Errorf("Could not parse the Consul query URL template (%s), skipping it. Error: %s", q.ServiceURL, err)
			continue
		}
		q.serviceURLTemplate = serviceURLTemplate

		// Allow to use join function in tags
		templateFunctions := template.FuncMap{"join": strings.Join}
		// Parse the tag value templates
		q.serviceExtraTagsTemplate = make(map[string]*template.Template)
		for tagName, tagTemplateString := range q.ServiceExtraTags {
			tagTemplate, err := template.New(tagName).Funcs(templateFunctions).Parse(tagTemplateString)
			if err != nil {
				p.Log.Errorf("Could not parse the Consul query Extra Tag template (%s), skipping it. Error: %s", tagTemplateString, err)
				continue
			}
			q.serviceExtraTagsTemplate[tagName] = tagTemplate
		}
		p.ConsulConfig.Queries[i] = q
		i++
	}
	// Prevent memory leak by erasing truncated values
	for j := i; j < len(p.ConsulConfig.Queries); j++ {
		p.ConsulConfig.Queries[j] = nil
	}
	p.ConsulConfig.Queries = p.ConsulConfig.Queries[:i]

	catalog := consul.Catalog()

	p.wg.Add(1)
	go func() {
		// Store last error status and change log level depending on repeated occurrence
		var refreshFailed = false
		defer p.wg.Done()
		err := p.refreshConsulServices(catalog)
		if err != nil {
			refreshFailed = true
			p.Log.Errorf("Unable to refresh Consul services: %v", err)
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(p.ConsulConfig.QueryInterval)):
				err := p.refreshConsulServices(catalog)
				if err != nil {
					message := fmt.Sprintf("Unable to refresh Consul services: %v", err)
					if refreshFailed {
						p.Log.Debug(message)
					} else {
						p.Log.Warn(message)
					}
					refreshFailed = true
				} else if refreshFailed {
					refreshFailed = false
					p.Log.Info("Successfully refreshed Consul services after previous errors")
				}
			}
		}
	}()

	return nil
}

func (p *Prometheus) refreshConsulServices(c *api.Catalog) error {
	consulServiceURLs := make(map[string]urlAndAddress)

	p.Log.Debugf("Refreshing Consul services")

	for _, q := range p.ConsulConfig.Queries {
		queryOptions := api.QueryOptions{}
		if q.ServiceDc != "" {
			queryOptions.Datacenter = q.ServiceDc
		}

		// Request services from Consul
		consulServices, _, err := c.Service(q.ServiceName, q.ServiceTag, &queryOptions)
		if err != nil {
			return err
		}
		if len(consulServices) == 0 {
			p.Log.Debugf("Queried Consul for Service (%s, %s) but did not find any instances", q.ServiceName, q.ServiceTag)
			continue
		}
		p.Log.Debugf("Queried Consul for Service (%s, %s) and found %d instances", q.ServiceName, q.ServiceTag, len(consulServices))

		for _, consulService := range consulServices {
			uaa, err := p.getConsulServiceURL(q, consulService)
			if err != nil {
				message := fmt.Sprintf("Unable to get scrape URLs from Consul for Service (%s, %s): %s", q.ServiceName, q.ServiceTag, err)
				if q.lastQueryFailed {
					p.Log.Debug(message)
				} else {
					p.Log.Warn(message)
				}
				q.lastQueryFailed = true
				break
			}
			if q.lastQueryFailed {
				p.Log.Infof("Created scrape URLs from Consul for Service (%s, %s)", q.ServiceName, q.ServiceTag)
			}
			q.lastQueryFailed = false
			p.Log.Debugf("Adding scrape URL from Consul for Service (%s, %s): %s", q.ServiceName, q.ServiceTag, uaa.url.String())
			consulServiceURLs[uaa.url.String()] = *uaa
		}
	}

	p.lock.Lock()
	p.consulServices = consulServiceURLs
	p.lock.Unlock()

	return nil
}

func (p *Prometheus) getConsulServiceURL(q *consulQuery, s *api.CatalogService) (*urlAndAddress, error) {
	var buffer bytes.Buffer
	buffer.Reset()
	err := q.serviceURLTemplate.Execute(&buffer, s)
	if err != nil {
		return nil, err
	}
	serviceURL, err := url.Parse(buffer.String())
	if err != nil {
		return nil, err
	}

	extraTags := make(map[string]string)
	for tagName, tagTemplate := range q.serviceExtraTagsTemplate {
		buffer.Reset()
		err = tagTemplate.Execute(&buffer, s)
		if err != nil {
			return nil, err
		}
		extraTags[tagName] = buffer.String()
	}

	p.Log.Debugf("Will scrape metrics from Consul Service %s", serviceURL.String())

	return &urlAndAddress{
		url:         serviceURL,
		originalURL: serviceURL,
		tags:        extraTags,
	}, nil
}
