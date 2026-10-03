import FreeCAD as App
doc = App.getDocument('FlangeCoupling')
obj = doc.getObject('FlangeRibbed')
shape = obj.Shape

# Try to fillet all external edges at R3
ext_idx = []
int_idx = []
for i, e in enumerate(shape.Edges):
    try:
        cog = e.CenterOfGravity
        r = (cog.x**2 + cog.y**2)**0.5
        if r > 55:
            ext_idx.append(i)
        else:
            int_idx.append(i)
    except:
        int_idx.append(i)

print(f'Ext: {len(ext_idx)}, Int: {len(int_idx)}')

# Create filleted shape
try:
    new_shape = shape.makeFillet(3.0, [shape.Edges[i] for i in ext_idx[:10]])
    print('External fillet OK')
    new_shape = new_shape.makeFillet(2.0, [new_shape.Edges[j] for j in range(min(50, len(new_shape.Edges)))])
    print('Internal fillet OK')
    
    fillet_obj = doc.addObject('Part::Feature', 'Filleted')
    fillet_obj.Shape = new_shape
    doc.recompute()
    print('Filleted shape created')
except Exception as e:
    print(f'Error: {e}')
