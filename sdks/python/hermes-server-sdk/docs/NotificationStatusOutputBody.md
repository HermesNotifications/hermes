# NotificationStatusOutputBody


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**var_schema** | **str** | A URL to the JSON Schema for this object. | [optional] [readonly] 
**events** | [**List[NotificationEvent]**](NotificationEvent.md) | Timeline of notification events | 
**notification** | [**Notification**](Notification.md) | The notification record | 

## Example

```python
from hermes_server_sdk.models.notification_status_output_body import NotificationStatusOutputBody

# TODO update the JSON string below
json = "{}"
# create an instance of NotificationStatusOutputBody from a JSON string
notification_status_output_body_instance = NotificationStatusOutputBody.from_json(json)
# print the JSON string representation of the object
print(NotificationStatusOutputBody.to_json())

# convert the object into a dict
notification_status_output_body_dict = notification_status_output_body_instance.to_dict()
# create an instance of NotificationStatusOutputBody from a dict
notification_status_output_body_from_dict = NotificationStatusOutputBody.from_dict(notification_status_output_body_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


