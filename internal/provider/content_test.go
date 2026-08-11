// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package provider

import "testing"

func TestBuiltins_ContentSchema(t *testing.T) {
	email, _ := Builtins.Channel(ChannelEmail)
	subject, ok := email.ContentFieldByKey("subject")
	if !ok || subject.Render != RenderText || subject.MapsTo != "title" {
		t.Fatalf("email subject field: got %+v ok=%v", subject, ok)
	}
	body, ok := email.ContentFieldByKey("body")
	if !ok || body.Render != RenderHTML || body.MapsTo != "body" {
		t.Fatalf("email body field: got %+v ok=%v", body, ok)
	}
	if _, ok := email.ContentFieldByKey("nope"); ok {
		t.Fatal("unknown content field should report ok=false")
	}

	sms, _ := Builtins.Channel(ChannelSMS)
	if len(sms.Content) != 1 || sms.Content[0].Key != "body" || sms.Content[0].Render != RenderText {
		t.Fatalf("sms content: got %+v", sms.Content)
	}

	inbox, _ := Builtins.Channel(ChannelInbox)
	title, ok := inbox.ContentFieldByKey("title")
	if !ok || title.MapsTo != "title" || title.Render != RenderText {
		t.Fatalf("inbox title field: got %+v ok=%v", title, ok)
	}
	bodyI, ok := inbox.ContentFieldByKey("body")
	if !ok || bodyI.MapsTo != "body" || bodyI.Render != RenderText {
		t.Fatalf("inbox body field: got %+v ok=%v", bodyI, ok)
	}
}
