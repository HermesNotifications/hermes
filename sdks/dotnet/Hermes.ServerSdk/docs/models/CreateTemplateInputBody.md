# Hermes.ServerSdk.Model.CreateTemplateInputBody

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Name** | **string** | Human-readable name | 
**Slug** | **string** | URL-friendly identifier | 
**Schema** | **string** | A URL to the JSON Schema for this object. | [optional] [readonly] 
**DefaultChannels** | **List&lt;string&gt;** | Default channels (used when no subscription) | [optional] 
**EmailBody** | **string** | Email body template (HTML) | [optional] 
**EmailSubject** | **string** | Email subject template | [optional] 
**InboxBody** | **string** | Inbox body template | [optional] 
**InboxTitle** | **string** | Inbox title template | [optional] 
**SmsBody** | **string** | SMS body template | [optional] 
**SubscriptionId** | **string** | Subscription ID (null for standalone) | [optional] 

[[Back to Model list]](../../README.md#documentation-for-models) [[Back to API list]](../../README.md#documentation-for-api-endpoints) [[Back to README]](../../README.md)

