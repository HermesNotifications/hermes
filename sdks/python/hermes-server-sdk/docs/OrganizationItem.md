# OrganizationItem


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**var_schema** | **str** | A URL to the JSON Schema for this object. | [optional] [readonly] 
**created_at** | **datetime** |  | 
**default_locale** | **str** |  | 
**id** | **str** |  | 
**name** | **str** |  | 
**user_count** | **int** |  | 

## Example

```python
from hermes_server_sdk.models.organization_item import OrganizationItem

# TODO update the JSON string below
json = "{}"
# create an instance of OrganizationItem from a JSON string
organization_item_instance = OrganizationItem.from_json(json)
# print the JSON string representation of the object
print(OrganizationItem.to_json())

# convert the object into a dict
organization_item_dict = organization_item_instance.to_dict()
# create an instance of OrganizationItem from a dict
organization_item_from_dict = OrganizationItem.from_dict(organization_item_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


