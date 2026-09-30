package main

import (
	"encoding/json"
	"log"
	"strings"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/talkingheads2053/talkingheads/pkg/commands"
)

// questionKind selects which MQTT topic and payload type a question is sent as.
type questionKind int

const (
	// kindDirection publishes commands.Direction to direction/<actor>.
	kindDirection questionKind = iota
	// kindSay publishes commands.Say to say/<actor>; the utterance is spoken
	// by Dialogue but is not added to any Actor's conversation history.
	kindSay
	// kindRespond publishes commands.Direction with Respond=true to
	// direction/<actor>, instructing the Actor to respond to the last speaker.
	kindRespond
)

type question struct {
	Content   string
	To        string
	Kind      questionKind
	RespondTo string
}

type conversation struct {
	client    mqtt.Client
	questions chan question
}

func newConversation(server string) (*conversation, error) {
	options := mqtt.NewClientOptions()
	options.AddBroker(server)
	options.SetClientID("moderator")
	options.KeepAlive = 300

	log.Printf("Connecting to MQTT broker at %s\n", server)
	client := mqtt.NewClient(options)
	token := client.Connect()
	if token.Wait() && token.Error() != nil {
		log.Fatal("failed creating MQTT client: ", token.Error())
		return nil, token.Error()
	}

	c := &conversation{
		client:    client,
		questions: make(chan question, 1),
	}

	return c, nil
}

func (c *conversation) processQuestions() error {
	for question := range c.questions {
		var topic string
		var payload []byte
		var err error

		switch question.Kind {
		case kindSay:
			topic = "say/" + question.To
			payload, err = json.Marshal(commands.Say{Who: question.To, What: question.Content})
		case kindRespond:
			topic = "direction/" + question.To
			payload, err = json.Marshal(commands.Direction{Who: question.To, What: question.Content, Respond: true, RespondTo: question.RespondTo})
		default:
			topic = "direction/" + question.To
			payload, err = json.Marshal(commands.Direction{Who: question.To, What: question.Content})
		}
		if err != nil {
			log.Printf("failed marshalling message: %v", err)
			continue
		}

		token := c.client.Publish(topic, 0, false, payload)
		if token.Wait() && token.Error() != nil {
			log.Fatal("failed publishing to MQTT topic: ", token.Error())
			return token.Error()
		}
	}

	return nil
}

// trimSurroundingQuotes removes a single pair of matching surrounding double
// quotes from s, if present. Whitespace is trimmed first so that leading or
// trailing spaces around the quotes are ignored.
func trimSurroundingQuotes(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	return s
}

func (c *conversation) Close() {
	c.client.Disconnect(250)
}
