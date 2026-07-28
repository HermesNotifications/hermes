# Hermes.ServerSdk.Model.TokenInputBody

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**OrganizationId** | **string** | Organization identifier | 
**UserId** | **string** | External user identifier | 
**Schema** | **string** | A URL to the JSON Schema for this object. | [optional] [readonly] 
**ExpiresIn** | **long** | Requested token lifetime in seconds (min 3600 &#x3D; 1h, max 604800 &#x3D; 7d, default 14400 &#x3D; 4h). The actual expiry includes ±10% random jitter to prevent thundering-herd token refreshes. | [optional] 

[[Back to Model list]](../../README.md#documentation-for-models) [[Back to API list]](../../README.md#documentation-for-api-endpoints) [[Back to README]](../../README.md)

