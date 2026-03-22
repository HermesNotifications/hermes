# NotificationGroup


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**var_schema** | **str** | A URL to the JSON Schema for this object. | [optional] [readonly] 
**created_at** | **datetime** |  | 
**default_channels** | **List[str]** |  | 
**id** | **str** |  | 
**name** | **str** |  | 
**slug** | **str** |  | 

## Example

```python
from hermes_server_sdk.models.notification_group import NotificationGroup

# TODO update the JSON string below
json = "{}"
# create an instance of NotificationGroup from a JSON string
notification_group_instance = NotificationGroup.from_json(json)
# print the JSON string representation of the object
print(NotificationGroup.to_json())

# convert the object into a dict
notification_group_dict = notification_group_instance.to_dict()
# create an instance of NotificationGroup from a dict
notification_group_from_dict = NotificationGroup.from_dict(notification_group_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


