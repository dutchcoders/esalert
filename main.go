package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"context"
	"github.com/patrickmn/go-cache"
	"gopkg.in/olivere/elastic.v5"
	"time"

	"flag"
	"github.com/BurntSushi/toml"
	"strconv"
	"text/template"
)

type Msg struct {
	Text        string       `json:"text,omitempty"`
	Username    string       `json:"username,omitempty"`
	IconEmoji   string       `json:"icon_emoji,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`

	Title     string `json:"title,omitempty"`
	TitleLink string `json:"title_link,omitempty"`
	TS        int64  `json:"ts,omitempty"`

	Markdown bool `json:"mrkdwn,omitempty"`
}

type Attachment struct {
	Text      string `json:"text,omitempty"`
	Title     string `json:"title,omitempty"`
	TitleLink string `json:"title_link,omitempty
"`
	TS         int64    `json:"ts,omitempty"`
	MarkdownIn []string `json:"mrkdwn_in,omitempty"`
}

type Alert struct {
	Hook      string
	Template  string
	Queries   []string
	Index     string
	Username  string
	DateField string `toml:"date_field"`
	IconEmoji string `toml:"icon_emoji"`
}

type Config struct {
	Alerts []Alert `toml:"alert"`
}

var configFile string

func init() {
	flag.StringVar(&configFile, "config", "config.toml", "specifies the location of the config file")
}

func main() {
	flag.Parse()

	var config Config
	if _, err := toml.DecodeFile(configFile, &config); err != nil {
		// handle error
		panic(err)
	}

	// Create a cache with a default expiration time of 5 minutes, and which
	// purges expired items every 30 seconds
	c := cache.New(30*time.Minute, 30*time.Second)

	es, err := elastic.NewClient(elastic.SetURL("http://127.0.0.1:9200/"), elastic.SetSniff(false))
	if err != nil {
		panic(err)
	}

	type Hit struct {
		Alert Alert
		Msg   Msg
	}

	hitChan := make(chan Hit)

	go func() {
		for {
			hit := <-hitChan

			body := &bytes.Buffer{}

			if err := json.NewEncoder(body).Encode(hit.Msg); err != nil {
				fmt.Println(err.Error())
			}

			req, err := http.NewRequest("POST", hit.Alert.Hook, bytes.NewBuffer(body.Bytes()))
			if err != nil {
				fmt.Println(err.Error())
			}

			req.Header.Add("Content-Type", "application/json")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				fmt.Println(err.Error())
			}

			_ = resp
		}
	}()

	lastDate := time.Now()

	for {
		for _, alert := range config.Alerts {
			queries := alert.Queries

			// search for keywords
			hl := elastic.NewHighlight()
			hl = hl.Fields(elastic.NewHighlighterField("*").RequireFieldMatch(false).NumOfFragments(15))
			hl = hl.PreTags("*").PostTags("*")

			fq := elastic.NewBoolQuery()
			fq = fq.Must(elastic.NewRangeQuery(alert.DateField).Gte(lastDate))
			fmt.Println(lastDate)

			// update last scanning date
			lastDate = time.Now().Add(-24 * time.Hour)

			for _, query := range queries {
				qs := elastic.NewBoolQuery()

				qs = qs.Must(elastic.NewQueryStringQuery(query))
				qs.Filter(fq)

				results, err := es.Search().
					Index(alert.Index).
					Highlight(hl).
					Query(qs).
					From(0).Size(100).
					Do(context.Background())
				if err != nil {
					fmt.Println(err.Error())
					continue
				}

				fmt.Printf("Query: %s, Results: %d.\n", query, len(results.Hits.Hits))

				for _, hit := range results.Hits.Hits {
					attachments := []Attachment{}
					for k, hl := range hit.Highlight {
						// k?
						_ = k

						for _, part := range hl {
							raw := part

							attachments = append(attachments, Attachment{
								Title:      k,
								Text:       raw,
								MarkdownIn: []string{"text", "pretext"},
							})
						}
					}

					var doc map[string]interface{}
					if err := json.Unmarshal(*hit.Source, &doc); err != nil {
						continue
					}

					if _, found := c.Get(hit.Id); found {
						continue
					}

					c.Set(hit.Id, doc, cache.DefaultExpiration)

					/*
						date := time.Now() // paste.Date.Unix()

						var fields interface{} = doc

						for _, part := range strings.Split(alert.DateField, ".") {
							if v, ok := fields.(map[string]interface{}); ok {
								fields = v[part]
							} else if v, ok := fields.(string); !ok {
							} else if t, err := time.Parse(time.RFC3339, v); err != nil {
							} else {
								date = t
							}
						}
					*/

					tmpl, err := template.New("").Funcs(template.FuncMap{
						"unix": func(args ...interface{}) string {
							if t, err := time.Parse(time.RFC3339, args[0].(string)); err != nil {
								return ""
							} else {
								return strconv.Itoa(int(t.Unix()))
							}
						},
					}).Parse(alert.Template)
					if err != nil {
						fmt.Println(err.Error())
						continue
					}

					var buff bytes.Buffer
					if err := tmpl.Execute(&buff, struct {
						ID       string
						Query    string
						Document map[string]interface{}
					}{
						ID:       hit.Id,
						Query:    query,
						Document: doc,
					}); err != nil {
						fmt.Println(err.Error())
						continue
					}

					msg := Msg{
						Text:        string(buff.Bytes()),
						Markdown:    true,
						Username:    alert.Username,
						IconEmoji:   alert.IconEmoji,
						Attachments: attachments,
						TS:          time.Now().Unix(),
					}

					hitChan <- Hit{
						Alert: alert,
						Msg:   msg,
					}
					break
				}
			}
		}

		time.Sleep(time.Minute * 1)
	}
}
