# Hermes.ServerSdk.Model.NotificationMetadata
Opaque metadata stored with the notification and echoed back. Hermes reads only 'level' and 'toast'; every other key round-trips untouched.

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Level** | **string** | How a client should present this notification. | [optional] 
**Toast** | **bool** | Whether a client should surface this transiently rather than waiting for the user to open their inbox. | [optional] 

[[Back to Model list]](../../README.md#documentation-for-models) [[Back to API list]](../../README.md#documentation-for-api-endpoints) [[Back to README]](../../README.md)

