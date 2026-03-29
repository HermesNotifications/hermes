# CreateCategoryInputBody


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**var_schema** | **str** | A URL to the JSON Schema for this object. | [optional] [readonly] 
**default_channels** | **List[str]** | Default delivery channels | [optional] 
**default_state** | **str** | Default subscription state | 
**name** | **str** | Human-readable name | 
**slug** | **str** | URL-friendly identifier | 
**sort_order** | **int** | Display order | [optional] 

## Example

```python
from hermes_server_sdk.models.create_category_input_body import CreateCategoryInputBody

# TODO update the JSON string below
json = "{}"
# create an instance of CreateCategoryInputBody from a JSON string
create_category_input_body_instance = CreateCategoryInputBody.from_json(json)
# print the JSON string representation of the object
print(CreateCategoryInputBody.to_json())

# convert the object into a dict
create_category_input_body_dict = create_category_input_body_instance.to_dict()
# create an instance of CreateCategoryInputBody from a dict
create_category_input_body_from_dict = CreateCategoryInputBody.from_dict(create_category_input_body_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


