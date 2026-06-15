# NotificationEvent


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**channel** | **str** |  | 
**created_at** | **datetime** |  | 
**event** | **str** |  | 
**id** | **str** |  | 
**metadata** | **object** |  | [optional] 
**notification_id** | **str** |  | 
**severity** | **str** |  | 

## Example

```python
from hermes_server_sdk.models.notification_event import NotificationEvent

# TODO update the JSON string below
json = "{}"
# create an instance of NotificationEvent from a JSON string
notification_event_instance = NotificationEvent.from_json(json)
# print the JSON string representation of the object
print(NotificationEvent.to_json())

# convert the object into a dict
notification_event_dict = notification_event_instance.to_dict()
# create an instance of NotificationEvent from a dict
notification_event_from_dict = NotificationEvent.from_dict(notification_event_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


