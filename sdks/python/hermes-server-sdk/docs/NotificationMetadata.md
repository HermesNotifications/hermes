# NotificationMetadata

Opaque metadata stored with the notification and echoed back. Hermes reads only 'level' and 'toast'; every other key round-trips untouched.

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**level** | **str** | How a client should present this notification. | [optional] 
**toast** | **bool** | Whether a client should surface this transiently rather than waiting for the user to open their inbox. | [optional] 

## Example

```python
from hermes_server_sdk.models.notification_metadata import NotificationMetadata

# TODO update the JSON string below
json = "{}"
# create an instance of NotificationMetadata from a JSON string
notification_metadata_instance = NotificationMetadata.from_json(json)
# print the JSON string representation of the object
print(NotificationMetadata.to_json())

# convert the object into a dict
notification_metadata_dict = notification_metadata_instance.to_dict()
# create an instance of NotificationMetadata from a dict
notification_metadata_from_dict = NotificationMetadata.from_dict(notification_metadata_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


