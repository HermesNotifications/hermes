# UpdateCategoryInputBody


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**var_schema** | **str** | A URL to the JSON Schema for this object. | [optional] [readonly] 
**default_channels** | **List[str]** | Default delivery channels | 
**default_state** | **str** | Default subscription state | 
**name** | **str** | Human-readable name | 
**sort_order** | **int** | Display order | 

## Example

```python
from hermes_server_sdk.models.update_category_input_body import UpdateCategoryInputBody

# TODO update the JSON string below
json = "{}"
# create an instance of UpdateCategoryInputBody from a JSON string
update_category_input_body_instance = UpdateCategoryInputBody.from_json(json)
# print the JSON string representation of the object
print(UpdateCategoryInputBody.to_json())

# convert the object into a dict
update_category_input_body_dict = update_category_input_body_instance.to_dict()
# create an instance of UpdateCategoryInputBody from a dict
update_category_input_body_from_dict = UpdateCategoryInputBody.from_dict(update_category_input_body_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


