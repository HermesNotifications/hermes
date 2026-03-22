# UpdateGroupInputBody


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**var_schema** | **str** | A URL to the JSON Schema for this object. | [optional] [readonly] 
**default_channels** | **List[str]** | Default delivery channels for this group | 
**name** | **str** | Human-readable name | 

## Example

```python
from hermes_server_sdk.models.update_group_input_body import UpdateGroupInputBody

# TODO update the JSON string below
json = "{}"
# create an instance of UpdateGroupInputBody from a JSON string
update_group_input_body_instance = UpdateGroupInputBody.from_json(json)
# print the JSON string representation of the object
print(UpdateGroupInputBody.to_json())

# convert the object into a dict
update_group_input_body_dict = update_group_input_body_instance.to_dict()
# create an instance of UpdateGroupInputBody from a dict
update_group_input_body_from_dict = UpdateGroupInputBody.from_dict(update_group_input_body_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


