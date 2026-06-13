package main

import (
	"log"
	"os"
	"time"

	"github.com/nats-io/nats.go"
)

func main() {
	natsHost := "localhost"
	if envHost := os.Getenv("NATS_HOST"); envHost != "" {
		natsHost = envHost
	}
	natsPort := "4222"
	if envPort := os.Getenv("NATS_PORT"); envPort != "" {
		natsPort = envPort
	}
	natsURL := "nats://" + natsHost + ":" + natsPort
	log.Printf("Connecting to NATS at %s...", natsURL)
	nc, err := nats.Connect(natsURL, nats.Timeout(10*time.Second))
	if err != nil {
		log.Fatalf("Failed to connect to NATS: %v", err)
	}
	defer nc.Close()

	js, err := nc.JetStream()
	if err != nil {
		log.Fatalf("Failed to get JetStream context: %v", err)
	}

	streamName := "common"
	subjects := []string{"resource.*", "user.*"}

	log.Printf("Checking if stream '%s' exists...", streamName)
	stream, err := js.StreamInfo(streamName)
	if err != nil {
		log.Printf("Stream '%s' does not exist. Creating it...", streamName)
		_, err = js.AddStream(&nats.StreamConfig{
			Name:     streamName,
			Subjects: subjects,
			Storage:  nats.FileStorage, // Persist on disk
		})
		if err != nil {
			log.Fatalf("Failed to add stream '%s': %v", streamName, err)
		}
		log.Printf("Stream '%s' created successfully!", streamName)
	} else {
		log.Printf("Stream '%s' already exists: %+v", streamName, stream)
	}

	// Create consumers to quiet the logs and allow subscriptions
	consumers := []struct {
		name    string
		subject string
	}{
		{"vault-resource-banned", "resource.banned"},
		{"web-ui-resource-vaulted", "resource.vaulted"},
		{"web-ui-resource-banned", "resource.banned"},
		{"web-ui-user-updated", "user.updated"},
	}

	for _, c := range consumers {
		log.Printf("Creating consumer '%s' bound to subject '%s'...", c.name, c.subject)
		_, err = js.AddConsumer(streamName, &nats.ConsumerConfig{
			Durable:       c.name,
			DeliverSubject: "", // Pull consumer
			FilterSubject: c.subject,
			AckPolicy:     nats.AckExplicitPolicy,
		})
		if err != nil {
			log.Printf("Warning: Failed to create consumer '%s': %v", c.name, err)
		} else {
			log.Printf("Consumer '%s' created successfully!", c.name)
		}
	}

	log.Println("NATS JetStream Initialization completed successfully!")
}
