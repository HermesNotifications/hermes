# UserItem


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**contacts** | **Dict[str, str]** |  | [optional] 
**created_at** | **datetime** |  | 
**external_id** | **str** |  | 
**id** | **str** |  | 
**locale** | **str** |  | 
**organization_id** | **str** |  | 
**organization_name** | **str** |  | 

## Example

```python
from hermes_server_sdk.models.user_item import UserItem

# TODO update the JSON string below
json = "{}"
# create an instance of UserItem from a JSON string
user_item_instance = UserItem.from_json(json)
# print the JSON string representation of the object
print(UserItem.to_json())

# convert the object into a dict
user_item_dict = user_item_instance.to_dict()
# create an instance of UserItem from a dict
user_item_from_dict = UserItem.from_dict(user_item_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


