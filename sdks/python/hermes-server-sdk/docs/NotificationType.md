# NotificationType


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**var_schema** | **str** | A URL to the JSON Schema for this object. | [optional] [readonly] 
**created_at** | **datetime** |  | 
**email_body** | **str** |  | [optional] 
**email_subject** | **str** |  | [optional] 
**group_id** | **str** |  | 
**id** | **str** |  | 
**inbox_body** | **str** |  | [optional] 
**inbox_title** | **str** |  | [optional] 
**name** | **str** |  | 
**slug** | **str** |  | 
**sms_body** | **str** |  | [optional] 

## Example

```python
from hermes_server_sdk.models.notification_type import NotificationType

# TODO update the JSON string below
json = "{}"
# create an instance of NotificationType from a JSON string
notification_type_instance = NotificationType.from_json(json)
# print the JSON string representation of the object
print(NotificationType.to_json())

# convert the object into a dict
notification_type_dict = notification_type_instance.to_dict()
# create an instance of NotificationType from a dict
notification_type_from_dict = NotificationType.from_dict(notification_type_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


