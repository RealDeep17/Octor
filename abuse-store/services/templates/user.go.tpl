Hi!

{{ if .Abuse | IsIllegal }} 
This torrent added to stoplist and will become unavailable for users in an hour.
All connected cached content will be purged as well.
{{ else }}
Thank you for your request! We will do our best to solve it!
{{ end }}

Notice ID: {{ .Abuse.NoticeID }}
Infohash:  {{ .Abuse.Infohash }}
Filename:  {{ .Abuse.Filename }}
Description:

{{ .Abuse.Description | EmailQuote }}

This message was generated automatically.

If you have any additional question please email directly to {{ .Support }}

Sincerely yours, Webtor Support Team.