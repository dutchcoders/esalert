# esalert
Elasticsearch alerting service to Slack

## Configuration
```
[[alert]]
hook="{hook}"
index="{index}"
queries=["keyword1", "keyword2"]
template='''Alert for query *{{.Query}}*: <LINK?id={{.ID}}|[open]>. <!date^{{.Document.received_date | unix}}^Posted {date_num} {time_secs}|Posted 2014-02-18 6:39:42 AM.>'''
username="alertbot"
icon_emoji=":ghost:"
date_field="received_date"
```
