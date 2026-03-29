# NotificationTemplate


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**var_schema** | **str** | A URL to the JSON Schema for this object. | [optional] [readonly] 
**created_at** | **datetime** |  | 
**default_channels** | **List[str]** |  | 
**email_body** | **str** |  | [optional] 
**email_subject** | **str** |  | [optional] 
**id** | **str** |  | 
**inbox_body** | **str** |  | [optional] 
**inbox_title** | **str** |  | [optional] 
**name** | **str** |  | 
**slug** | **str** |  | 
**sms_body** | **str** |  | [optional] 
**subscription_id** | **str** |  | [optional] 

## Example

```python
from hermes_server_sdk.models.notification_template import NotificationTemplate

# TODO update the JSON string below
json = "{}"
# create an instance of NotificationTemplate from a JSON string
notification_template_instance = NotificationTemplate.from_json(json)
# print the JSON string representation of the object
print(NotificationTemplate.to_json())

# convert the object into a dict
notification_template_dict = notification_template_instance.to_dict()
# create an instance of NotificationTemplate from a dict
notification_template_from_dict = NotificationTemplate.from_dict(notification_template_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


