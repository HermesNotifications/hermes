# SubscriptionCategory


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**var_schema** | **str** | A URL to the JSON Schema for this object. | [optional] [readonly] 
**created_at** | **datetime** |  | 
**default_channels** | **List[str]** |  | 
**default_state** | **str** |  | 
**id** | **str** |  | 
**name** | **str** |  | 
**slug** | **str** |  | 
**sort_order** | **int** |  | 

## Example

```python
from hermes_server_sdk.models.subscription_category import SubscriptionCategory

# TODO update the JSON string below
json = "{}"
# create an instance of SubscriptionCategory from a JSON string
subscription_category_instance = SubscriptionCategory.from_json(json)
# print the JSON string representation of the object
print(SubscriptionCategory.to_json())

# convert the object into a dict
subscription_category_dict = subscription_category_instance.to_dict()
# create an instance of SubscriptionCategory from a dict
subscription_category_from_dict = SubscriptionCategory.from_dict(subscription_category_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


