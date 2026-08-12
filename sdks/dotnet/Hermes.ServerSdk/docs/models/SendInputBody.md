# Hermes.ServerSdk.Model.SendInputBody

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**To** | [**SendRecipient**](SendRecipient.md) | Notification recipient | 
**Schema** | **string** | A URL to the JSON Schema for this object. | [optional] [readonly] 
**Channels** | **List&lt;string&gt;** | Explicit delivery channels | [optional] 
**Content** | [**SendContent**](SendContent.md) | Direct content (mutually exclusive with template) | [optional] 
**Data** | **Dictionary&lt;string, Object&gt;** | Template data for rendering | [optional] 
**Metadata** | [**SendInputBodyMetadata**](SendInputBodyMetadata.md) |  | [optional] 
**Template** | **string** | Notification template slug (mutually exclusive with content) | [optional] 

[[Back to Model list]](../../README.md#documentation-for-models) [[Back to API list]](../../README.md#documentation-for-api-endpoints) [[Back to README]](../../README.md)

