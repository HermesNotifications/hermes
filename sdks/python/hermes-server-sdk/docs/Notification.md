# Notification


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**action_label** | **str** |  | [optional] 
**action_url** | **str** |  | [optional] 
**archived_at** | **datetime** |  | [optional] 
**body** | **str** |  | 
**category_id** | **str** |  | 
**channels** | **List[str]** |  | 
**created_at** | **datetime** |  | 
**deleted_at** | **datetime** |  | [optional] 
**delivered_at** | **datetime** |  | [optional] 
**id** | **str** |  | 
**idempotency_key** | **str** |  | [optional] 
**read_at** | **datetime** |  | [optional] 
**sent_at** | **datetime** |  | [optional] 
**status** | **str** |  | 
**template_id** | **str** |  | [optional] 
**tenant_id** | **str** |  | 
**title** | **str** |  | 
**user_id** | **str** |  | 

## Example

```python
from hermes_server_sdk.models.notification import Notification

# TODO update the JSON string below
json = "{}"
# create an instance of Notification from a JSON string
notification_instance = Notification.from_json(json)
# print the JSON string representation of the object
print(Notification.to_json())

# convert the object into a dict
notification_dict = notification_instance.to_dict()
# create an instance of Notification from a dict
notification_from_dict = Notification.from_dict(notification_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


