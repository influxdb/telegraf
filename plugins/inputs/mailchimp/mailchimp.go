//go:generate ../../../tools/readme_config_includer/generator
package mailchimp

import (
	_ "embed"
	"fmt"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

type MailChimp struct {
	APIKey     string          `toml:"api_key"`
	DaysOld    int             `toml:"days_old"`
	CampaignID string          `toml:"campaign_id"`
	Log        telegraf.Logger `toml:"-"`

	api *chimpAPI
}

func (*MailChimp) SampleConfig() string {
	return sampleConfig
}

func (m *MailChimp) Init() error {
	m.api = newChimpAPI(m.APIKey, m.Log)

	return nil
}

func (m *MailChimp) Gather(acc telegraf.Accumulator) error {
	if m.CampaignID == "" {
		since := ""
		if m.DaysOld > 0 {
			now := time.Now()
			d, err := time.ParseDuration(fmt.Sprintf("%dh", 24*m.DaysOld))
			if err != nil {
				return err
			}
			since = now.Add(-d).Format(time.RFC3339)
		}

		reports, err := m.api.getReports(reportsParams{
			sinceSendTime: since,
		})
		if err != nil {
			return err
		}
		now := time.Now()

		for _, report := range reports.Reports {
			gatherReport(acc, report, now)
		}
	} else {
		report, err := m.api.getReport(m.CampaignID)
		if err != nil {
			return err
		}
		now := time.Now()
		gatherReport(acc, report, now)
	}

	return nil
}

func gatherReport(acc telegraf.Accumulator, report report, now time.Time) {
	tags := make(map[string]string)
	tags["id"] = report.ID
	tags["campaign_title"] = report.CampaignTitle
	fields := map[string]interface{}{
		"emails_sent":              report.EmailsSent,
		"abuse_reports":            report.AbuseReports,
		"unsubscribed":             report.Unsubscribed,
		"hard_bounces":             report.Bounces.HardBounces,
		"soft_bounces":             report.Bounces.SoftBounces,
		"syntax_errors":            report.Bounces.SyntaxErrors,
		"forwards_count":           report.Forwards.ForwardsCount,
		"forwards_opens":           report.Forwards.ForwardsOpens,
		"opens_total":              report.Opens.OpensTotal,
		"unique_opens":             report.Opens.UniqueOpens,
		"open_rate":                report.Opens.OpenRate,
		"clicks_total":             report.Clicks.ClicksTotal,
		"unique_clicks":            report.Clicks.UniqueClicks,
		"unique_subscriber_clicks": report.Clicks.UniqueSubscriberClicks,
		"click_rate":               report.Clicks.ClickRate,
		"facebook_recipient_likes": report.FacebookLikes.RecipientLikes,
		"facebook_unique_likes":    report.FacebookLikes.UniqueLikes,
		"facebook_likes":           report.FacebookLikes.FacebookLikes,
		"industry_type":            report.IndustryStats.Type,
		"industry_open_rate":       report.IndustryStats.OpenRate,
		"industry_click_rate":      report.IndustryStats.ClickRate,
		"industry_bounce_rate":     report.IndustryStats.BounceRate,
		"industry_unopen_rate":     report.IndustryStats.UnopenRate,
		"industry_unsub_rate":      report.IndustryStats.UnsubRate,
		"industry_abuse_rate":      report.IndustryStats.AbuseRate,
		"list_stats_sub_rate":      report.ListStats.SubRate,
		"list_stats_unsub_rate":    report.ListStats.UnsubRate,
		"list_stats_open_rate":     report.ListStats.OpenRate,
		"list_stats_click_rate":    report.ListStats.ClickRate,
	}
	acc.AddFields("mailchimp", fields, tags, now)
}

func init() {
	inputs.Add("mailchimp", func() telegraf.Input {
		return &MailChimp{}
	})
}
