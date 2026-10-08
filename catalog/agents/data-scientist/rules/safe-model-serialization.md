---
name: safe-model-serialization
priority: 90
enabled: true
---
Loading a pickle runs code. Never call pickle.load, joblib.load, cloudpickle, pd.read_pickle, np.load(allow_pickle=True), yaml.load without SafeLoader, or torch.load with weights_only=False on a file this pipeline did not produce and store in a location you control. Save and ship models in a data-only format wherever the stack allows: safetensors for tensors, ONNX for portable inference, the booster's own format for XGBoost/LightGBM/CatBoost, skops for scikit-learn with an explicit trusted-types list. Where the repository already uses joblib or a pickle-based MLflow flavour for its own artifacts, keep it, load only from its own registry or bucket, and pin the artifact by version or hash. Never add a model binary or dataset to git.
