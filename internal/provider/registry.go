// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package provider

import "fmt"

// Registry is the source of truth that de-hardcodes channel/provider knowledge
// out of dispatch. It is populated once (see Builtins) and read concurrently
// thereafter; it is not safe for concurrent registration.
type Registry struct {
	channels  map[string]ChannelDescriptor
	providers map[string]Manifest // keyed by provider ID
	byChannel map[string][]string // channel slug -> ordered provider IDs
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		channels:  map[string]ChannelDescriptor{},
		providers: map[string]Manifest{},
		byChannel: map[string][]string{},
	}
}

// RegisterChannel adds a channel descriptor. Panics on an empty or duplicate slug.
func (r *Registry) RegisterChannel(d ChannelDescriptor) {
	if d.Slug == "" {
		panic("provider: channel descriptor has empty slug")
	}
	if _, dup := r.channels[d.Slug]; dup {
		panic(fmt.Sprintf("provider: channel %q already registered", d.Slug))
	}
	r.channels[d.Slug] = d
}

// Channel returns the descriptor for slug and whether it exists.
func (r *Registry) Channel(slug string) (ChannelDescriptor, bool) {
	d, ok := r.channels[slug]
	return d, ok
}

// RegisterProvider adds a provider manifest under its channel. Panics on an
// empty/duplicate ID or an unregistered channel.
func (r *Registry) RegisterProvider(m Manifest) {
	if m.ID == "" {
		panic("provider: manifest has empty ID")
	}
	if _, ok := r.channels[m.Channel]; !ok {
		panic(fmt.Sprintf("provider: provider %q references unregistered channel %q", m.ID, m.Channel))
	}
	if _, dup := r.providers[m.ID]; dup {
		panic(fmt.Sprintf("provider: provider %q already registered", m.ID))
	}
	r.providers[m.ID] = m
	r.byChannel[m.Channel] = append(r.byChannel[m.Channel], m.ID)
}

// ProvidersForChannel returns the ordered provider IDs registered for a channel
// (nil if none).
func (r *Registry) ProvidersForChannel(channel string) []string {
	return r.byChannel[channel]
}

// IsAddressKey reports whether key is a contact address key declared by some
// registered channel (e.g. "email", "phone"). The empty key (channels needing
// no external address, like inbox) is never valid.
func (r *Registry) IsAddressKey(key string) bool {
	if key == "" {
		return false
	}
	for _, d := range r.channels {
		if d.AddressKey == key {
			return true
		}
	}
	return false
}
