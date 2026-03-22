# Hermes.ServerSdk.Model.SendInputBody

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**TenantId** | **string** | Tenant identifier | 
**UserId** | **string** | External user identifier | 
**Schema** | **string** | A URL to the JSON Schema for this object. | [optional] [readonly] 
**Channels** | **List&lt;string&gt;** | Explicit delivery channels | [optional] 
**Content** | [**SendContent**](SendContent.md) | Direct content (mutually exclusive with type) | [optional] 
**Data** | **Dictionary&lt;string, Object&gt;** | Template data for rendering | [optional] 
**Group** | **string** | Group slug (required for direct content sends) | [optional] 
**Type** | **string** | Notification type slug (mutually exclusive with content) | [optional] 

[[Back to Model list]](../../README.md#documentation-for-models) [[Back to API list]](../../README.md#documentation-for-api-endpoints) [[Back to README]](../../README.md)

