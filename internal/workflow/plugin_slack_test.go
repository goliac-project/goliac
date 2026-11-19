package workflow

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"encoding/json"

	"github.com/stretchr/testify/assert"
)

func TestSlackPluginWorkflow(t *testing.T) {
	t.Run("happy path: slack message", func(t *testing.T) {

		httpTest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			var sBody SlackMessage
			err := json.Unmarshal(body, &sBody)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"errorMessages":["Channel is required"]}`))
				return
			}

			if sBody.Channel == "" {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"errorMessages":["Channel is required"]}`))
			} else {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"ok":true}`))
			}
		}))
		defer httpTest.Close()

		plugin := StepPluginSlack{
			SlackUrl:   httpTest.URL,
			SlackToken: "123456",
			Channel:    "mychannel",
		}

		url, err := plugin.Execute(context.Background(), "foo", "workflowdescription", "explanation", &url.URL{}, map[string]interface{}{
			"channel": "mychannel",
		})

		assert.Nil(t, err)
		assert.Equal(t, "", url)
	})
}

func TestSlackPluginEscapesUserText(t *testing.T) {
	var got SlackMessage
	httpTest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer httpTest.Close()
	plugin := StepPluginSlack{SlackUrl: httpTest.URL, SlackToken: "t", Channel: "c"}
	_, err := plugin.Execute(context.Background(), "a<b", "desc & more", "<!channel> see <https://evil.example|click> & go", &url.URL{Path: "/o/r/pull/1"}, nil)
	assert.Nil(t, err)
	assert.Contains(t, got.Text, "&lt;!channel&gt; see &lt;https://evil.example|click&gt; &amp; go")
	assert.Contains(t, got.Text, "a&lt;b")
	assert.Contains(t, got.Text, "desc &amp; more")
	assert.NotContains(t, got.Text, "<!channel>")
	assert.Equal(t, "a&amp;&lt;&gt;", slackEscape("a&<>"))
}
