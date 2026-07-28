# NotificationItem


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**body** | **str** |  | 
**category_id** | **str** |  | 
**channels** | **List[str]** |  | 
**created_at** | **datetime** |  | 
**id** | **str** |  | 
**organization_id** | **str** |  | 
**status** | **str** |  | 
**template_id** | **str** |  | [optional] 
**template_slug** | **str** |  | [optional] 
**title** | **str** |  | 
**user_id** | **str** |  | 

## Example

```python
from hermes_server_sdk.models.notification_item import NotificationItem

# TODO update the JSON string below
json = "{}"
# create an instance of NotificationItem from a JSON string
notification_item_instance = NotificationItem.from_json(json)
# print the JSON string representation of the object
print(NotificationItem.to_json())

# convert the object into a dict
notification_item_dict = notification_item_instance.to_dict()
# create an instance of NotificationItem from a dict
notification_item_from_dict = NotificationItem.from_dict(notification_item_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


